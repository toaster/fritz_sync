package sync

import (
	"log"
	"reflect"
)

// Contact represents a synchronisable contact record.
type Contact struct {
	Email    string
	FullName string
	ID       string
	Image    string
	Numbers  []PhoneNumber
	SyncID   string
}

// PhoneNumber represents a phone number including priority, type and purpose.
type PhoneNumber struct {
	Number   string
	Priority bool
	Purpose  PhonePurpose
	Type     PhoneType
}

// PhoneType describes the type of the device a phone number belongs to (e.g. cell or fax).
type PhoneType int

// The known phone device types.
const (
	_ PhoneType = iota // Voice
	Cell
	Fax
	// Not supported(?) in FritzBox -> Sync has to use the values supported by _all_ adapters.
	// Video
	// Pager
	// Text
	// Textphone
)

// PhonePurpose describes the purpose of a phone number, i.e. if it is for home or work usage.
type PhonePurpose int

// The known phone number purposes.
const (
	Home PhonePurpose = iota
	Work
)

// Reader provides read access to contacts stored on a backend (e.g. CardDAV or Fritz!Box).
type Reader interface {
	// ReadAll reads all contacts, optionally restricted to a list of categories.
	ReadAll([]string) (map[string]Contact, error)
}

// Writer provides write access to contacts stored on a backend (e.g. CardDAV or Fritz!Box).
type Writer interface {
	// Add adds all given contacts.
	Add([]Contact) error
	// Delete removes all given contacts.
	Delete([]Contact) error
	// Update updates all given contacts.
	Update([]Contact) error
}

// ReaderWriter combines read and write access to a contact storage.
type ReaderWriter interface {
	Reader
	Writer
}

// Sync reads all contacts from “from” and adds or updates the appropriate contacts in “to” if necessary.
func Sync(from []Reader, to ReaderWriter, categories []string, log *log.Logger) error {
	if log != nil {
		log.Println("Read target records…")
	}
	old, err := to.ReadAll([]string{})
	if err != nil {
		return err
	}
	if log != nil {
		log.Println("Amount of target records:", len(old))
	}

	if log != nil {
		log.Println("Read source records…")
	}
	newContacts := map[string]Contact{}
	for _, r := range from {
		n, err := r.ReadAll(categories)
		if err != nil {
			return err
		}
		for k, c := range n {
			newContacts[k] = c
		}
	}
	if log != nil {
		log.Println("Amount of source records:", len(newContacts))
	}

	var toBeDeleted []Contact
	var toBeAdded []Contact
	var toBeUpdated []Contact

	for _, oldContact := range old {
		newContact, ok := newContacts[oldContact.SyncID]
		if ok {
			delete(newContacts, oldContact.SyncID)
			if !equal(oldContact, newContact) {
				newContact.SyncID = newContact.ID
				newContact.ID = oldContact.ID
				toBeUpdated = append(toBeUpdated, newContact)
			}
		} else {
			toBeDeleted = append(toBeDeleted, oldContact)
		}
	}
	for _, newContact := range newContacts {
		newContact.SyncID = newContact.ID
		newContact.ID = ""
		toBeAdded = append(toBeAdded, newContact)
	}

	if log != nil {
		log.Println("Delete", len(toBeDeleted), "records…")
	}
	if err := to.Delete(toBeDeleted); err != nil {
		return err
	}
	if log != nil {
		log.Println("Update", len(toBeUpdated), "records…")
	}
	if err := to.Update(toBeUpdated); err != nil {
		return err
	}
	if log != nil {
		log.Println("Add", len(toBeAdded), "records…")
	}
	if err := to.Add(toBeAdded); err != nil {
		return err
	}

	if log != nil {
		log.Println("Done")
	}
	return nil
}

func equal(a, b Contact) bool {
	return a.Email == b.Email &&
		a.FullName == b.FullName &&
		a.Image == b.Image &&
		((len(a.Numbers) == 0 && len(b.Numbers) == 0) || reflect.DeepEqual(a.Numbers, b.Numbers))
}
