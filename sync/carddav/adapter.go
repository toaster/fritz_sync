package carddav

import (
	"io"
	"os"
	"strings"

	"github.com/emersion/go-vcard"
	"github.com/studio-b12/gowebdav"

	"github.com/toaster/fritz_sync/sync"
)

// Adapter implements the sync.Reader interface for accessing CardDAV contacts.
type Adapter struct {
	client *gowebdav.Client
}

// NewAdapter creates a new Adapter for a given CardDAV URL and the corresponding credentials.
func NewAdapter(contactsURL, user, pass string) *Adapter {
	return &Adapter{gowebdav.NewClient(contactsURL, user, pass)}
}

// ReadAll reads all contacts (part of sync.Reader interface).
func (a *Adapter) ReadAll(categories []string) (map[string]sync.Contact, error) {
	files, err := a.client.ReadDir("/")
	if err != nil {
		return nil, err
	}

	contacts := map[string]sync.Contact{}
	for _, file := range files {
		if err := a.readFile(file, categories, contacts); err != nil {
			return nil, err
		}
	}
	return contacts, nil
}

func (a *Adapter) readFile(file os.FileInfo, categories []string, contacts map[string]sync.Contact) error {
	reader, err := a.client.ReadStream(file.Name())
	if err != nil {
		return err
	}
	defer reader.Close()

	dec := vcard.NewDecoder(reader)
	for {
		card, err := dec.Decode()
		if err == io.EOF {
			break
		} else if err != nil {
			return err
		}

		addContact := true
		if len(categories) > 0 {
			addContact = false
			for _, cat := range card.Categories() {
				for _, useCat := range categories {
					if cat == useCat {
						addContact = true
						break
					}
				}
				if addContact {
					break
				}
			}
		}
		if addContact {
			contact := contactFromCard(card)
			contacts[contact.ID] = contact
		}
	}
	return nil
}

func contactFromCard(card vcard.Card) sync.Contact {
	contact := sync.Contact{
		FullName: strings.TrimSpace(card.PreferredValue(vcard.FieldFormattedName)),
		Email:    strings.TrimSpace(card.PreferredValue(vcard.FieldEmail)),
		ID:       strings.TrimSpace(card.Value(vcard.FieldUID)),
		Image:    card.PreferredValue(vcard.FieldPhoto),
		Numbers:  []sync.PhoneNumber{},
	}
	preferredNumberSet := false
	for _, field := range card[vcard.FieldTelephone] {
		number := phoneNumberFromField(field)
		contact.Numbers = append(contact.Numbers, number)
		if number.Priority {
			preferredNumberSet = true
		}
	}
	// make sure preferred number is always set
	if len(contact.Numbers) > 0 && !preferredNumberSet {
		contact.Numbers[0].Priority = true
	}
	return contact
}

func phoneNumberFromField(field *vcard.Field) sync.PhoneNumber {
	number := sync.PhoneNumber{Number: strings.TrimSpace(field.Value)}
	for _, typ := range field.Params[vcard.ParamType] {
		switch strings.ToLower(typ) {
		case vcard.TypeHome:
			number.Purpose = sync.Home
		case vcard.TypeWork:
			number.Purpose = sync.Work
		case vcard.TypeCell:
			number.Type = sync.Cell
		case vcard.TypeFax:
			number.Type = sync.Fax
		// -> see definition of PhoneType values in sync package
		// case vcard.TypeText:
		// 	number.Type = sync.Text
		// case vcard.TypeVideo:
		// 	number.Type = sync.Video
		// case vcard.TypePager:
		// 	number.Type = sync.Pager
		// case vcard.TypeTextPhone:
		// 	number.Type = sync.Textphone
		case "pref":
			number.Priority = true
		}
	}
	if pref := field.Params.Get(vcard.ParamPreferred); pref != "" && pref != "0" {
		number.Priority = true
	}
	return number
}
