package digest

// Copyright 2013 M-Lab
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// The digest package provides an implementation of http.RoundTripper that takes
// care of HTTP Digest Authentication (http://www.ietf.org/rfc/rfc2617.txt).
// This only implements the MD5 and "auth" portions of the RFC, but that covers
// the majority of available server side implementations including apache web
// server.
//
// Example usage:
//
//	t := NewTransport("myUserName", "myP@55w0rd")
//	req, err := http.NewRequest("GET", "http://notreal.com/path?arg=1", nil)
//	if err != nil {
//		return err
//	}
//	resp, err := t.RoundTrip(req)
//	if err != nil {
//		return err
//	}
//
// OR it can be used as a client:
//
//	c, err := t.Client()
//	if err != nil {
//		return err
//	}
//	resp, err := c.Get("http://notreal.com/path?arg=1")
//	if err != nil {
//		return err
//	}
//

import (
	"bytes"
	"crypto/md5"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"strings"
)

// error constants
var (
	ErrNilTransport      = errors.New("transport is nil")
	ErrBadChallenge      = errors.New("challenge is bad")
	ErrAlgNotImplemented = errors.New("alg not implemented")
)

// Transport is an implementation of http.RoundTripper that takes care of http
// digest authentication.
type Transport struct {
	Username  string
	Password  string
	Transport http.RoundTripper
	auth      string
}

// NewTransport creates a new digest transport using the http.DefaultTransport.
func NewTransport(username, password string) *Transport {
	t := &Transport{
		Username: username,
		Password: password,
	}
	t.Transport = http.DefaultTransport
	return t
}

type challenge struct {
	Realm     string
	Domain    string
	Nonce     string
	Opaque    string
	Stale     string
	Algorithm string
	Qop       string
}

func parseChallenge(input string) (*challenge, error) {
	const ws = " \n\r\t"
	const qs = `"`
	s := strings.Trim(input, ws)
	if !strings.HasPrefix(s, "Digest ") {
		return nil, ErrBadChallenge
	}
	s = strings.Trim(s[7:], ws)
	sl := strings.Split(s, ",")
	c := &challenge{
		Algorithm: "MD5",
	}
	var r []string
	for i := range sl {
		r = strings.SplitN(sl[i], "=", 2)
		switch strings.Trim(r[0], " ") {
		case "realm":
			c.Realm = strings.Trim(r[1], qs)
		case "domain":
			c.Domain = strings.Trim(r[1], qs)
		case "nonce":
			c.Nonce = strings.Trim(r[1], qs)
		case "opaque":
			c.Opaque = strings.Trim(r[1], qs)
		case "stale":
			c.Stale = strings.Trim(r[1], qs)
		case "algorithm":
			c.Algorithm = strings.Trim(r[1], qs)
		case "qop":
			// TODO(gavaletz) should be an array of strings?
			c.Qop = strings.Trim(r[1], qs)
		default:
			return nil, ErrBadChallenge
		}
	}
	return c, nil
}

type credentials struct {
	Username   string
	Realm      string
	Nonce      string
	DigestURI  string
	Algorithm  string
	Cnonce     string
	Opaque     string
	MessageQop string
	NonceCount int
	method     string
	password   string
}

func h(data string) string {
	hf := md5.New()
	if _, err := io.WriteString(hf, data); err != nil {
		log.Println("Failed to write MD5 hash:", err)
	}
	return fmt.Sprintf("%x", hf.Sum(nil))
}

func kd(secret, data string) string {
	return h(fmt.Sprintf("%s:%s", secret, data))
}

func (c *credentials) ha1() string {
	return h(fmt.Sprintf("%s:%s:%s", c.Username, c.Realm, c.password))
}

func (c *credentials) ha2() string {
	return h(fmt.Sprintf("%s:%s", c.method, c.DigestURI))
}

func (c *credentials) resp(cnonce string) (string, error) {
	c.NonceCount++
	if c.MessageQop == "auth" {
		if cnonce != "" {
			c.Cnonce = cnonce
		} else {
			b := make([]byte, 8)
			if _, err := io.ReadFull(rand.Reader, b); err != nil {
				log.Println("Failed to read random bytes:", err)
			}
			c.Cnonce = fmt.Sprintf("%x", b)[:16]
		}
		return kd(c.ha1(), fmt.Sprintf("%s:%08x:%s:%s:%s",
			c.Nonce, c.NonceCount, c.Cnonce, c.MessageQop, c.ha2())), nil
	} else if c.MessageQop == "" {
		return kd(c.ha1(), fmt.Sprintf("%s:%s", c.Nonce, c.ha2())), nil
	}
	return "", ErrAlgNotImplemented
}

func (c *credentials) authorize() (string, error) {
	// Note that this is only implemented for MD5 and NOT MD5-sess.
	// MD5-sess is rarely supported and those that do are a big mess.
	if c.Algorithm != "MD5" {
		return "", ErrAlgNotImplemented
	}
	// Note that this is NOT implemented for "qop=auth-int".  Similarly the
	// auth-int server side implementations that do exist are a mess.
	if c.MessageQop != "auth" && c.MessageQop != "" {
		return "", ErrAlgNotImplemented
	}
	resp, err := c.resp("")
	if err != nil {
		return "", ErrAlgNotImplemented
	}
	sl := []string{fmt.Sprintf(`username="%s"`, c.Username)}
	sl = append(sl, fmt.Sprintf(`realm="%s"`, c.Realm))
	sl = append(sl, fmt.Sprintf(`nonce="%s"`, c.Nonce))
	sl = append(sl, fmt.Sprintf(`uri="%s"`, c.DigestURI))
	sl = append(sl, fmt.Sprintf(`response="%s"`, resp))
	if c.Algorithm != "" {
		sl = append(sl, fmt.Sprintf(`algorithm="%s"`, c.Algorithm))
	}
	if c.Opaque != "" {
		sl = append(sl, fmt.Sprintf(`opaque="%s"`, c.Opaque))
	}
	if c.MessageQop != "" {
		sl = append(sl, fmt.Sprintf("qop=%s", c.MessageQop))
		sl = append(sl, fmt.Sprintf("nc=%08x", c.NonceCount))
		sl = append(sl, fmt.Sprintf(`cnonce="%s"`, c.Cnonce))
	}
	return fmt.Sprintf("Digest %s", strings.Join(sl, ", ")), nil
}

func (t *Transport) newCredentials(req *http.Request, c *challenge) *credentials {
	return &credentials{
		Username:   t.Username,
		Realm:      c.Realm,
		Nonce:      c.Nonce,
		DigestURI:  req.URL.RequestURI(),
		Algorithm:  c.Algorithm,
		Opaque:     c.Opaque,
		MessageQop: c.Qop, // "auth" must be a single value
		NonceCount: 0,
		method:     req.Method,
		password:   t.Password,
	}
}

// RoundTrip makes a request expecting a 401 response that will require digest
// authentication.  It creates the credentials it needs and makes a follow-up
// request.
func (t *Transport) RoundTrip(req *http.Request) (*http.Response, error) {
	if t.Transport == nil {
		return nil, ErrNilTransport
	}

	body := bytes.NewBuffer([]byte{})
	// reuse auth
	if t.auth != "" {
		req.Header.Set("Authorization", t.auth)
	}
	req.Body = newTeeReadCloser(req.Body, body)
	resp, err := t.Transport.RoundTrip(req)
	// response did not require auth
	if err != nil || resp.StatusCode != 401 {
		return resp, err
	}

	challenge, err := parseChallenge(resp.Header.Get("WWW-Authenticate"))
	if err != nil {
		return resp, fmt.Errorf("failed to parse challenge: %v", err)
	}

	// form credentials based on the auth
	cr := t.newCredentials(req, challenge)
	t.auth, err = cr.authorize()
	if err != nil {
		return resp, err
	}
	// We'll no longer use the initial response, so close it.
	resp.Body.Close()

	// retry with new auth
	authReq := new(http.Request)
	*authReq = *req
	authReq.Header = make(http.Header)
	for k, s := range req.Header {
		authReq.Header[k] = s
	}
	authReq.Header.Set("Authorization", t.auth)
	authReq.Body = ioutil.NopCloser(body)
	return t.Transport.RoundTrip(authReq)
}

// Client returns an HTTP client that uses the digest transport.
func (t *Transport) Client() (*http.Client, error) {
	if t.Transport == nil {
		return nil, ErrNilTransport
	}
	return &http.Client{Transport: t}, nil
}

type teeReadCloser struct {
	rc io.ReadCloser
	tr io.Reader
}

func newTeeReadCloser(rc io.ReadCloser, w io.Writer) io.ReadCloser {
	return &teeReadCloser{rc: rc, tr: io.TeeReader(rc, w)}
}

func (t teeReadCloser) Read(p []byte) (n int, err error) {
	return t.tr.Read(p)
}

func (t teeReadCloser) Close() error {
	return t.rc.Close()
}
