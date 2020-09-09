package fritzbox

import (
	"bytes"
	"encoding/base64"
	"encoding/xml"
	"fmt"
	"io"
	"net/url"
	"strconv"
	"strings"

	"github.com/huin/goupnp/soap"
	"github.com/jlaffaye/ftp"

	"github.com/toaster/fritz_sync/sync"
	"github.com/toaster/fritz_sync/tr064"
)

// Adapter implements the sync.Reader interface for accessing Fritz!Box contacts.
type Adapter struct {
	ftpHost      string
	ftpPass      string
	ftpUser      string
	ns           string
	pbID         string
	pixStorage   string
	syncIDKey    string
	tr064Adapter *tr064.Adapter
}

type fritzPbPerson struct {
	ImgURL   string             `xml:"imageURL"`
	RealName string             `xml:"realName"`
	Unknown  []tr064.UnknownXML `xml:",any"`
}

type fritzPbNumber struct {
	ID        int    `xml:"id,attr"`
	Number    string `xml:",chardata"`
	Prio      int    `xml:"prio,attr"`
	QuickDial int    `xml:"quickdial,attr,omitempty"`
	Type      string `xml:"type,attr"`
	Vanity    string `xml:"vanity,attr,omitempty"`
}

type fritzPbTelephony struct {
	NID     int                `xml:"nid,attr"`
	NNID    int                `xml:"nnid,attr"`
	Numbers []fritzPbNumber    `xml:"number"`
	Unknown []tr064.UnknownXML `xml:",any"`
}

type fritzPbEmail struct {
	Address string `xml:",chardata"`
	ID      string `xml:"id,attr"`
	Type    string `xml:"classifier,attr"`
}

type fritzPhonebookEntry struct {
	XMLName   xml.Name           `xml:"contact"`
	Category  int                `xml:"category"`
	Email     fritzPbEmail       `xml:"services>email"`
	Features  string             `xml:"features"`
	Modtime   int                `xml:"mod_time"`
	Person    fritzPbPerson      `xml:"person"`
	Setup     string             `xml:"setup"`
	Telephony fritzPbTelephony   `xml:"telephony"`
	UniqueID  int                `xml:"uniqueid"`
	Unknown   []tr064.UnknownXML `xml:",any"`
}

const (
	errorInvalidArrayIndex = "713"
	errorInternalError     = "820"
)

const imgURLPrefix = "file:///var/InternerSpeicher"

// NewAdapter creates a new Adapter for a given Fritz!Box URL and the corresponding credentials.
func NewAdapter(boxURL, phonebookName, user, pass, storageName, syncIDKey string) (*Adapter, error) {
	uri, err := url.Parse(boxURL)
	if err != nil {
		return nil, fmt.Errorf("cannot parse Fritz!Box URL: %v", err)
	}

	describeURL := boxURL + "/tr64desc.xml"
	var desc tr064.Description
	if err := tr064.FetchXML(describeURL, &desc); err != nil {
		return nil, err
	}

	var telService *tr064.Service
	for _, service := range desc.Device.Services {
		if service.Type == "urn:dslforum-org:service:X_AVM-DE_OnTel:1" {
			telService = &service
			break
		}
	}
	if telService == nil {
		return nil, fmt.Errorf("%s does not provide a X_AVM-DE_OnTel:1 service", boxURL)
	}

	var scpd tr064.SCPD
	if err := tr064.FetchXML(boxURL+telService.ScpdURL, &scpd); err != nil {
		return nil, err
	}
	// TODO: check scpd for required Function definitions

	tr064Adapter, err := tr064.NewAdapter(boxURL, telService.ControlURL, user, pass)
	if err != nil {
		return nil, err
	}

	adapter := &Adapter{
		ftpHost:      uri.Hostname(),
		ftpPass:      pass,
		ftpUser:      user,
		ns:           telService.Type,
		pixStorage:   storageName,
		syncIDKey:    syncIDKey,
		tr064Adapter: tr064Adapter,
	}

	pbIDs, err := adapter.getPhonebookList()
	if err != nil {
		return nil, err
	}
	for _, pbID := range pbIDs {
		name, err := adapter.getPhonebook(pbID)
		if err != nil {
			return nil, err
		}
		if name == phonebookName {
			adapter.pbID = pbID
			break
		}
	}

	if adapter.pbID == "" {
		return nil, fmt.Errorf("could not find phonebook “%s” on %s", phonebookName, boxURL)
	}

	return adapter, nil
}

// ReadAll reads all contacts (part of sync.Reader interface).
func (a *Adapter) ReadAll(_ []string) (map[string]sync.Contact, error) {
	contacts := map[string]sync.Contact{}
	for i := 0; ; i++ {
		data, err := a.getPhonebookEntry(i)
		if err != nil {
			if serr, ok := err.(*soap.SOAPFaultError); ok {
				if serr.FaultCode == "s:Client" && serr.FaultString == "UPnPError" {
					var upnpError tr064.UPNPError
					if err := xml.Unmarshal(serr.Detail.Raw, &upnpError); err != nil {
						return nil, err
					}
					// Fritz!OS 7.20 on Fritz!Box 7590 returns 820 (internal) instead of 713 (invalid index)
					if upnpError.Code == errorInvalidArrayIndex || upnpError.Code == errorInternalError {
						break
					}
				}
			}
			return nil, err
		}
		contact, err := a.contactFromPhonebookEntry(data)
		if err != nil {
			return nil, err
		}
		contacts[contact.ID] = contact
	}
	return contacts, nil
}

// Add writes all given contacts into the phonebook (part of sync.Writer interface).
func (a *Adapter) Add(contacts []sync.Contact) error {
	for _, contact := range contacts {
		entry, err := a.phonebookEntryFromContact(contact)
		if err != nil {
			return err
		}
		if _, err := a.setPhonebookEntry(entry); err != nil {
			return err
		}
	}
	return nil
}

// Delete removes all given contacts from the phonebook (part of sync.Writer interface).
func (a *Adapter) Delete(contacts []sync.Contact) error {
	for _, contact := range contacts {
		if err := a.deletePhonebookEntry(contact.ID); err != nil {
			return err
		}
	}
	return nil
}

// Update updates all given contacts in the phonebook (part of sync.Writer interface).
func (a *Adapter) Update(contacts []sync.Contact) error {
	return a.Add(contacts)
}

func (a *Adapter) contactFromPhonebookEntry(entry *fritzPhonebookEntry) (sync.Contact, error) {
	contact := sync.Contact{
		FullName: strings.TrimSpace(entry.Person.RealName),
		Email:    strings.TrimSpace(entry.Email.Address),
		ID:       strconv.Itoa(entry.UniqueID),
	}
	for _, num := range entry.Telephony.Numbers {
		number := sync.PhoneNumber{
			Number:   strings.TrimSpace(num.Number),
			Priority: num.Prio > 0,
		}
		switch num.Type {
		case "home":
			number.Purpose = sync.Home
		case "work":
			number.Purpose = sync.Work
		case "mobile":
			number.Type = sync.Cell
		case "fax":
			number.Type = sync.Fax
		}
		contact.Numbers = append(contact.Numbers, number)
	}
	for _, e := range entry.Unknown {
		if e.XMLName.Local == a.syncIDKey {
			contact.SyncID = e.Inner
			break
		}
	}
	img, err := a.downloadImage(entry.Person.ImgURL)
	if err != nil {
		return sync.Contact{}, err
	}
	contact.Image = img
	return contact, nil
}

func (a *Adapter) deletePhonebookEntry(uniqueID string) error {
	params := struct {
		NewPhonebookID            string
		NewPhonebookEntryUniqueID string
	}{
		NewPhonebookID:            a.pbID,
		NewPhonebookEntryUniqueID: uniqueID,
	}
	if err := a.tr064Adapter.Perform(a.ns, "DeletePhonebookEntryUID", &params, nil); err != nil {
		return err
	}
	return nil
}

func (a *Adapter) downloadImage(imgURL string) (string, error) {
	if imgURL == "" {
		return "", nil
	}

	ftpConn, err := a.ftpConn()
	if err != nil {
		return "", err
	}
	defer func() { _ = ftpConn.Quit() }()

	imgPath := a.imgPathForImgURL(imgURL)
	imgReader, err := ftpConn.Retr(imgPath)
	if err != nil {
		return "", fmt.Errorf("cannot download image: %v", err)
	}
	buf := new(bytes.Buffer)
	encoder := base64.NewEncoder(base64.StdEncoding, buf)
	if _, err := io.Copy(encoder, imgReader); err != nil {
		return "", fmt.Errorf("cannot download/encode image: %v", err)
	}
	if err := encoder.Close(); err != nil {
		return "", fmt.Errorf("cannot encode image: %v", err)
	}

	return buf.String(), nil
}

func (a *Adapter) ftpConn() (*ftp.ServerConn, error) {
	ftpConn, err := ftp.Dial(
		a.ftpHost + ":21",
		// TLS deactivated because it is not stable on upload (Fritz!OS 7.20).
		// ftp.DialWithExplicitTLS(&tls.Config{ServerName: a.ftpHost}),
		// ftp.DialWithDebugOutput(os.Stdout),
	)
	if err != nil {
		return nil, fmt.Errorf("cannot connect to FTP server: %v", err)
	}

	if err := ftpConn.Login(a.ftpUser, a.ftpPass); err != nil {
		_ = ftpConn.Quit()
		return nil, fmt.Errorf("cannot log into FTP server: %v", err)
	}
	return ftpConn, nil
}

func (a *Adapter) getDECTHandsetInfo(id string) (string, string, error) {
	params := struct{ NewDectID string }{NewDectID: id}
	result := struct {
		NewHandsetName string
		NewPhonebookID string
	}{}
	if err := a.tr064Adapter.Perform(a.ns, "GetDECTHandsetInfo", &params, &result); err != nil {
		return "", "", err
	}
	return result.NewHandsetName, result.NewPhonebookID, nil
}

func (a *Adapter) getDECTHandsetList() (string, error) {
	result := struct{ NewDectIDList string }{}
	if err := a.tr064Adapter.Perform(a.ns, "GetDECTHandsetList", nil, &result); err != nil {
		return "", err
	}
	return result.NewDectIDList, nil
}

func (a *Adapter) getNumberOfEntries() (string, error) {
	result := struct{ NewOnTelNumberOfEntries string }{}
	if err := a.tr064Adapter.Perform(a.ns, "GetNumberOfEntries", nil, &result); err != nil {
		return "", err
	}
	return result.NewOnTelNumberOfEntries, nil
}

func (a *Adapter) getPhonebook(id string) (string, error) {
	params := struct{ NewPhonebookID string }{NewPhonebookID: id}
	result := struct {
		NewPhonebookName    string
		NewPhonebookExtraID string
		NewPhonebookURL     string
	}{}
	if err := a.tr064Adapter.Perform(a.ns, "GetPhonebook", &params, &result); err != nil {
		return "", err
	}
	return result.NewPhonebookName, nil
}

func (a *Adapter) getPhonebookEntry(index int) (*fritzPhonebookEntry, error) {
	params := struct {
		NewPhonebookID      string
		NewPhonebookEntryID string
	}{
		NewPhonebookID:      a.pbID,
		NewPhonebookEntryID: strconv.Itoa(index),
	}
	result := struct{ NewPhonebookEntryData string }{}
	if err := a.tr064Adapter.Perform(a.ns, "GetPhonebookEntry", &params, &result); err != nil {
		return nil, err
	}
	var entry fritzPhonebookEntry
	if err := xml.Unmarshal([]byte(result.NewPhonebookEntryData), &entry); err != nil {
		return nil, err
	}

	return &entry, nil
}

func (a *Adapter) getPhonebookList() ([]string, error) {
	result := struct{ NewPhonebookList string }{}
	if err := a.tr064Adapter.Perform(a.ns, "GetPhonebookList", nil, &result); err != nil {
		return nil, err
	}
	return strings.Split(result.NewPhonebookList, ","), nil
}

func (a *Adapter) imgPathForID(id string) string {
	pixPath := "/FRITZ/fonpix"
	if a.pixStorage != "" {
		pixPath = "/" + a.pixStorage + pixPath
	}
	imgPath := pixPath + "/" + id
	return imgPath
}

func (a *Adapter) imgPathForImgURL(imgURL string) string {
	return strings.TrimPrefix(imgURL, imgURLPrefix)
}

func (a *Adapter) imgURLForImgPath(imgPath string) string {
	return imgURLPrefix + imgPath
}

func (a *Adapter) phonebookEntryFromContact(contact sync.Contact) (*fritzPhonebookEntry, error) {
	entry := fritzPhonebookEntry{
		Person: fritzPbPerson{RealName: contact.FullName},
		Email:  fritzPbEmail{Address: contact.Email},
	}
	if contact.ID != "" {
		id, err := strconv.Atoi(contact.ID)
		if err != nil {
			return nil, err
		}
		entry.UniqueID = id
	}
	for _, num := range contact.Numbers {
		number := fritzPbNumber{Number: num.Number}
		if num.Priority {
			number.Prio = 1
		}
		if num.Type == sync.Fax {
			number.Type = "fax"
		} else if num.Type == sync.Cell {
			number.Type = "mobile"
		} else if num.Purpose == sync.Work {
			number.Type = "work"
		} else {
			number.Type = "home"
		}
		entry.Telephony.Numbers = append(entry.Telephony.Numbers, number)
	}
	if contact.SyncID != "" {
		entry.Unknown = []tr064.UnknownXML{
			{
				XMLName: xml.Name{Local: a.syncIDKey},
				Inner:   contact.SyncID,
			},
		}
	}
	if contact.Image != "" {
		imgURL, err := a.uploadImage(contact.SyncID, contact.Image)
		if err != nil {
			return nil, err
		}
		entry.Person.ImgURL = imgURL
	}
	return &entry, nil
}

func (a *Adapter) setPhonebookEntry(entry *fritzPhonebookEntry) (string, error) {
	data, err := xml.Marshal(entry)
	if err != nil {
		return "", err
	}
	params := struct {
		NewPhonebookID        string
		NewPhonebookEntryData string
	}{
		NewPhonebookID:        a.pbID,
		NewPhonebookEntryData: xml.Header + string(data),
	}
	result := struct{ NewPhonebookEntryUniqueID string }{}
	if err := a.tr064Adapter.Perform(a.ns, "SetPhonebookEntryUID", &params, &result); err != nil {
		return "", err
	}
	return result.NewPhonebookEntryUniqueID, nil
}

func (a *Adapter) uploadImage(id, image string) (string, error) {
	ftpConn, err := a.ftpConn()
	if err != nil {
		return "", err
	}
	defer func() { _ = ftpConn.Quit() }()

	imgPath := a.imgPathForID(id)
	imgReader := base64.NewDecoder(base64.StdEncoding, strings.NewReader(image))
	if err := ftpConn.Stor(imgPath, imgReader); err != nil {
		return "", fmt.Errorf("cannot upload image: %v", err)
	}

	return a.imgURLForImgPath(imgPath), nil
}
