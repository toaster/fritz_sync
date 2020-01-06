package fritzbox

import (
	"encoding/xml"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/huin/goupnp/soap"
	"github.com/toaster/digest"

	"github.com/toaster/fritz_sync/sync"
)

// Adapter implements the sync.Reader interface for accessing Fritz!Box contacts.
type Adapter struct {
	httpClient *http.Client
	soapClient *soap.SOAPClient
	ns         string
	pbID       string
	syncIDKey  string
}

type unknownXML struct {
	XMLName xml.Name `xml:""`
	Inner   string   `xml:",innerxml"`
}

type tr64SpecVersion struct {
	Major   int          `xml:"major"`
	Minor   int          `xml:"minor"`
	Unknown []unknownXML `xml:",any"`
}

type tr64SystemVersion struct {
	Major   int          `xml:"Major"`
	Minor   int          `xml:"Minor"`
	Patch   int          `xml:"Patch"`
	HW      int          `xml:"HW"`
	Build   int          `xml:"Buildnumber"`
	Display string       `xml:"Display"`
	Unknown []unknownXML `xml:",any"`
}

type tr64Icon struct {
	Mimetype string       `xml:"mimetype"`
	Width    int          `xml:"width"`
	Height   int          `xml:"height"`
	Depth    int          `xml:"depth"`
	URL      string       `xml:"url"`
	Unknown  []unknownXML `xml:",any"`
}

type tr64Service struct {
	Type        string       `xml:"serviceType"`
	ID          string       `xml:"serviceId"`
	ControlURL  string       `xml:"controlURL"`
	EventSubURL string       `xml:"eventSubURL"`
	ScpdURL     string       `xml:"SCPDURL"`
	Unknown     []unknownXML `xml:",any"`
}

type tr64Device struct {
	Type            string        `xml:"deviceType"`
	FriendlyName    string        `xml:"friendlyName"`
	Manufacturer    string        `xml:"manufacturer"`
	ManufacturerURL string        `xml:"manufacturerURL"`
	Description     string        `xml:"modelDescription"`
	Name            string        `xml:"modelName"`
	Number          string        `xml:"modelNumber"`
	URL             string        `xml:"modelURL"`
	UDN             string        `xml:"UDN"`
	UPC             string        `xml:"UPC"`
	Icons           []tr64Icon    `xml:"iconList>icon"`
	Services        []tr64Service `xml:"serviceList>service"`
	Devices         []tr64Device  `xml:"deviceList>device"`
	PresentationURL string        `xml:"presentationURL"`
	Unknown         []unknownXML  `xml:",any"`
}

type tr64Desc struct {
	XMLName       xml.Name          `xml:"urn:dslforum-org:device-1-0 root"`
	SpecVersion   tr64SpecVersion   `xml:"specVersion"`
	SystemVersion tr64SystemVersion `xml:"systemVersion"`
	Device        tr64Device        `xml:"device"`
	Unknown       []unknownXML      `xml:",any"`
}

type tr64ActionArg struct {
	Name          string       `xml:"name"`
	Direction     string       `xml:"direction"`
	StateVariable string       `xml:"relatedStateVariable"`
	Unknown       []unknownXML `xml:",any"`
}

type tr64Action struct {
	Name      string          `xml:"name"`
	Arguments []tr64ActionArg `xml:"argumentList>argument"`
	Unknown   []unknownXML    `xml:",any"`
}

type tr64StateVariableSpec struct {
	Name          string       `xml:"name"`
	DataType      string       `xml:"dataType"`
	DefaultValue  string       `xml:"defaultValue"`
	AllowedValues []string     `xml:"allowedValueList>allowedValue"`
	Unknown       []unknownXML `xml:",any"`
}

type tr64SCPD struct {
	XMLName           xml.Name                `xml:"urn:dslforum-org:service-1-0 scpd"`
	SpecVersion       tr64SpecVersion         `xml:"specVersion"`
	Actions           []tr64Action            `xml:"actionList>action"`
	ServiceStateSpecs []tr64StateVariableSpec `xml:"serviceStateTable>stateVariable"`
	Unknown           []unknownXML            `xml:",any"`
}

type fritzPbPerson struct {
	ImgURL   string       `xml:"imageURL"`
	RealName string       `xml:"realName"`
	Unknown  []unknownXML `xml:",any"`
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
	NID     int             `xml:"nid,attr"`
	NNID    int             `xml:"nnid,attr"`
	Numbers []fritzPbNumber `xml:"number"`
	Unknown []unknownXML    `xml:",any"`
}

type fritzPbEmail struct {
	Address string `xml:",chardata"`
	ID      string `xml:"id,attr"`
	Type    string `xml:"classifier,attr"`
}

type fritzPhonebookEntry struct {
	XMLName   xml.Name         `xml:"contact"`
	Category  int              `xml:"category"`
	Email     fritzPbEmail     `xml:"services>email"`
	Features  string           `xml:"features"`
	Modtime   int              `xml:"mod_time"`
	Person    fritzPbPerson    `xml:"person"`
	Setup     string           `xml:"setup"`
	Telephony fritzPbTelephony `xml:"telephony"`
	UniqueID  int              `xml:"uniqueid"`
	Unknown   []unknownXML     `xml:",any"`
}

type fritzUPNPError struct {
	XMLName     xml.Name `xml:"urn:dslforum-org:control-1-0 UPnPError"`
	Code        string   `xml:"errorCode"`
	Description string   `xml:"errorDescription"`
}

// NewAdapter creates a new Adapter for a given Fritz!Box URL and the corresponding credentials.
func NewAdapter(boxURL, phonebookName, user, pass, syncIDKey string) (*Adapter, error) {
	describeURL := boxURL + "/tr64desc.xml"

	var desc tr64Desc
	if err := fetchXML(describeURL, &desc); err != nil {
		return nil, err
	}

	var telService *tr64Service
	for _, service := range desc.Device.Services {
		if service.Type == "urn:dslforum-org:service:X_AVM-DE_OnTel:1" {
			telService = &service
			break
		}
	}
	if telService == nil {
		return nil, fmt.Errorf("%s does not provide a X_AVM-DE_OnTel:1 service", boxURL)
	}

	var scpd tr64SCPD
	if err := fetchXML(boxURL+telService.ScpdURL, &scpd); err != nil {
		return nil, err
	}
	// TODO: check scpd for required Function definitions

	controlURL, err := url.Parse(boxURL + telService.ControlURL)
	if err != nil {
		return nil, err
	}

	httpClient := http.Client{Transport: digest.NewTransport(user, pass)}
	adapter := &Adapter{
		httpClient: &httpClient,
		soapClient: &soap.SOAPClient{EndpointURL: *controlURL, HTTPClient: httpClient},
		ns:         telService.Type,
		syncIDKey:  syncIDKey,
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
					var upnpError fritzUPNPError
					if err := xml.Unmarshal(serr.Detail.Raw, &upnpError); err != nil {
						return nil, err
					}
					if upnpError.Code == "713" {
						// index out of bounds
						break
					}
				}
			}
			return nil, err
		}
		contact := a.contactFromPhonebookEntry(data)
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

func (a *Adapter) contactFromPhonebookEntry(entry *fritzPhonebookEntry) sync.Contact {
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
	// TODO Image
	return contact
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
		entry.Unknown = []unknownXML{
			{
				XMLName: xml.Name{Local: a.syncIDKey},
				Inner:   contact.SyncID,
			},
		}
	}
	// TODO Image
	return &entry, nil
}

func fetchXML(url string, result interface{}) error {
	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if err := xml.Unmarshal(body, result); err != nil {
		return err
	}
	return nil
}

func (a *Adapter) getPhonebook(id string) (string, error) {
	params := struct{ NewPhonebookID string }{NewPhonebookID: id}
	result := struct {
		NewPhonebookName    string
		NewPhonebookExtraID string
		NewPhonebookURL     string
	}{}
	if err := a.soapClient.PerformAction(a.ns, "GetPhonebook", &params, &result); err != nil {
		return "", err
	}
	return result.NewPhonebookName, nil
}

func (a *Adapter) getNumberOfEntries() (string, error) {
	result := struct{ NewOnTelNumberOfEntries string }{}
	if err := a.soapClient.PerformAction(a.ns, "GetNumberOfEntries", nil, &result); err != nil {
		return "", err
	}
	return result.NewOnTelNumberOfEntries, nil
}

func (a *Adapter) getPhonebookList() ([]string, error) {
	result := struct{ NewPhonebookList string }{}
	if err := a.soapClient.PerformAction(a.ns, "GetPhonebookList", nil, &result); err != nil {
		return nil, err
	}
	return strings.Split(result.NewPhonebookList, ","), nil
}

func (a *Adapter) getDECTHandsetList() (string, error) {
	result := struct{ NewDectIDList string }{}
	if err := a.soapClient.PerformAction(a.ns, "GetDECTHandsetList", nil, &result); err != nil {
		return "", err
	}
	return result.NewDectIDList, nil
}

func (a *Adapter) getDECTHandsetInfo(id string) (string, string, error) {
	params := struct{ NewDectID string }{NewDectID: id}
	result := struct {
		NewHandsetName string
		NewPhonebookID string
	}{}
	if err := a.soapClient.PerformAction(a.ns, "GetDECTHandsetInfo", &params, &result); err != nil {
		return "", "", err
	}
	return result.NewHandsetName, result.NewPhonebookID, nil
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
	if err := a.soapClient.PerformAction(a.ns, "GetPhonebookEntry", &params, &result); err != nil {
		return nil, err
	}
	var entry fritzPhonebookEntry
	if err := xml.Unmarshal([]byte(result.NewPhonebookEntryData), &entry); err != nil {
		return nil, err
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
	if err := a.soapClient.PerformAction(a.ns, "SetPhonebookEntryUID", &params, &result); err != nil {
		return "", err
	}
	return result.NewPhonebookEntryUniqueID, nil
}

func (a *Adapter) deletePhonebookEntry(uniqueID string) error {
	params := struct {
		NewPhonebookID            string
		NewPhonebookEntryUniqueID string
	}{
		NewPhonebookID:            a.pbID,
		NewPhonebookEntryUniqueID: uniqueID,
	}
	if err := a.soapClient.PerformAction(a.ns, "DeletePhonebookEntryUID", &params, nil); err != nil {
		return err
	}
	return nil
}
