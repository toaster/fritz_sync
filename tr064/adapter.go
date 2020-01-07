package tr064

import (
	"encoding/xml"
	"io/ioutil"
	"net/http"
	"net/url"

	"github.com/huin/goupnp/soap"
	"github.com/toaster/digest"
)

// Adapter is a generic TR064 adapter.
type Adapter struct {
	httpClient *http.Client
	soapClient *soap.SOAPClient
}

// UnknownXML collects unexpected XML into a string.
type UnknownXML struct {
	XMLName xml.Name `xml:""`
	Inner   string   `xml:",innerxml"`
}

type specVersion struct {
	Major   int          `xml:"major"`
	Minor   int          `xml:"minor"`
	Unknown []UnknownXML `xml:",any"`
}

type systemVersion struct {
	Major   int          `xml:"Major"`
	Minor   int          `xml:"Minor"`
	Patch   int          `xml:"Patch"`
	HW      int          `xml:"HW"`
	Build   int          `xml:"Buildnumber"`
	Display string       `xml:"Display"`
	Unknown []UnknownXML `xml:",any"`
}

type icon struct {
	Mimetype string       `xml:"mimetype"`
	Width    int          `xml:"width"`
	Height   int          `xml:"height"`
	Depth    int          `xml:"depth"`
	URL      string       `xml:"url"`
	Unknown  []UnknownXML `xml:",any"`
}

// Service describes a TR064 service.
type Service struct {
	Type        string       `xml:"serviceType"`
	ID          string       `xml:"serviceId"`
	ControlURL  string       `xml:"controlURL"`
	EventSubURL string       `xml:"eventSubURL"`
	ScpdURL     string       `xml:"SCPDURL"`
	Unknown     []UnknownXML `xml:",any"`
}

type device struct {
	Type            string       `xml:"deviceType"`
	FriendlyName    string       `xml:"friendlyName"`
	Manufacturer    string       `xml:"manufacturer"`
	ManufacturerURL string       `xml:"manufacturerURL"`
	Description     string       `xml:"modelDescription"`
	Name            string       `xml:"modelName"`
	Number          string       `xml:"modelNumber"`
	URL             string       `xml:"modelURL"`
	UDN             string       `xml:"UDN"`
	UPC             string       `xml:"UPC"`
	Icons           []icon       `xml:"iconList>icon"`
	Services        []Service    `xml:"serviceList>Service"`
	Devices         []device     `xml:"deviceList>device"`
	PresentationURL string       `xml:"presentationURL"`
	Unknown         []UnknownXML `xml:",any"`
}

// Description describes a TR064 interface.
type Description struct {
	XMLName       xml.Name      `xml:"urn:dslforum-org:device-1-0 root"`
	SpecVersion   specVersion   `xml:"specVersion"`
	SystemVersion systemVersion `xml:"systemVersion"`
	Device        device        `xml:"device"`
	Unknown       []UnknownXML  `xml:",any"`
}

type actionArg struct {
	Name          string       `xml:"name"`
	Direction     string       `xml:"direction"`
	StateVariable string       `xml:"relatedStateVariable"`
	Unknown       []UnknownXML `xml:",any"`
}

type action struct {
	Name      string       `xml:"name"`
	Arguments []actionArg  `xml:"argumentList>argument"`
	Unknown   []UnknownXML `xml:",any"`
}

type stateVariableSpec struct {
	Name          string       `xml:"name"`
	DataType      string       `xml:"dataType"`
	DefaultValue  string       `xml:"defaultValue"`
	AllowedValues []string     `xml:"allowedValueList>allowedValue"`
	Unknown       []UnknownXML `xml:",any"`
}

// SCPD describes TR064 service control actions.
type SCPD struct {
	XMLName           xml.Name            `xml:"urn:dslforum-org:Service-1-0 scpd"`
	SpecVersion       specVersion         `xml:"specVersion"`
	Actions           []action            `xml:"actionList>action"`
	ServiceStateSpecs []stateVariableSpec `xml:"serviceStateTable>stateVariable"`
	Unknown           []UnknownXML        `xml:",any"`
}

// UPNPError describes a uPNP error of a TR064 service control request.
type UPNPError struct {
	XMLName     xml.Name `xml:"urn:dslforum-org:control-1-0 UPnPError"`
	Code        string   `xml:"errorCode"`
	Description string   `xml:"errorDescription"`
}

// FetchXML fetches an XML document via an HTTP request and parses the response.
func FetchXML(url string, result interface{}) error {
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

// NewAdapter creates a new Adapter for a given base URL, Service control URL and the corresponding credentials.
func NewAdapter(baseURL, svcCtrlURL, user, pass string) (*Adapter, error) {
	controlURL, err := url.Parse(baseURL + svcCtrlURL)
	if err != nil {
		return nil, err
	}

	httpClient := http.Client{Transport: digest.NewTransport(user, pass)}
	adapter := &Adapter{
		httpClient: &httpClient,
		soapClient: &soap.SOAPClient{EndpointURL: *controlURL, HTTPClient: httpClient},
	}

	return adapter, nil
}

// Perform performs a TR064 action.
func (a *Adapter) Perform(ns, action string, params, result interface{}) error {
	return a.soapClient.PerformAction(ns, action, &params, &result)
}
