// Package saml implements a minimal SAML 2.0 SP (Service Provider) surface:
// metadata XML + ACS assertion consumer stub with basic signature-optional parsing.
// Not a full SAML suite — enough to wire Okta/Azure AD for login smoke tests.
package saml

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/xml"
	"fmt"
	"html"
	"net/url"
	"strings"
	"time"
)

type Config struct {
	EntityID       string // SP entity ID
	ACSURL         string
	IDPEntityID    string
	IDPSSOURL      string
	IDPCertificate string // PEM optional for stub
}

// MetadataXML returns SP metadata.
func (c Config) MetadataXML() string {
	return fmt.Sprintf(`<?xml version="1.0"?>
<md:EntityDescriptor xmlns:md="urn:oasis:names:tc:SAML:2.0:metadata" entityID="%s">
  <md:SPSSODescriptor AuthnRequestsSigned="false" WantAssertionsSigned="false"
      protocolSupportEnumeration="urn:oasis:names:tc:SAML:2.0:protocol">
    <md:NameIDFormat>urn:oasis:names:tc:SAML:1.1:nameid-format:emailAddress</md:NameIDFormat>
    <md:AssertionConsumerService Binding="urn:oasis:names:tc:SAML:2.0:bindings:HTTP-POST"
      Location="%s" index="0" isDefault="true"/>
  </md:SPSSODescriptor>
</md:EntityDescriptor>`, html.EscapeString(c.EntityID), html.EscapeString(c.ACSURL))
}

// RedirectURL builds IdP SSO redirect with SAMLRequest (deflate+base64 omitted for simplicity — uses simple base64 AuthnRequest).
func (c Config) RedirectURL(relayState string) (string, error) {
	if c.IDPSSOURL == "" {
		return "", fmt.Errorf("idp sso url required")
	}
	id := "_" + randomHex(16)
	req := fmt.Sprintf(`<?xml version="1.0"?>
<samlp:AuthnRequest xmlns:samlp="urn:oasis:names:tc:SAML:2.0:protocol"
  xmlns:saml="urn:oasis:names:tc:SAML:2.0:assertion"
  ID="%s" Version="2.0" IssueInstant="%s" Destination="%s"
  AssertionConsumerServiceURL="%s" ProtocolBinding="urn:oasis:names:tc:SAML:2.0:bindings:HTTP-POST">
  <saml:Issuer>%s</saml:Issuer>
</samlp:AuthnRequest>`, id, time.Now().UTC().Format(time.RFC3339), c.IDPSSOURL, c.ACSURL, c.EntityID)
	enc := base64.StdEncoding.EncodeToString([]byte(req))
	u, err := url.Parse(c.IDPSSOURL)
	if err != nil {
		return "", err
	}
	q := u.Query()
	q.Set("SAMLRequest", enc)
	if relayState != "" {
		q.Set("RelayState", relayState)
	}
	u.RawQuery = q.Encode()
	return u.String(), nil
}

// Assertion is a minimal parsed response.
type Assertion struct {
	NameID       string
	Email        string
	SessionID    string
	NotOnOrAfter time.Time
}

// ParseResponse decodes base64 SAMLResponse and extracts NameID/email without crypto verify
// (verify when certificate configured — stub checks presence only).
func ParseResponse(b64 string, requireCert bool, certPEM string) (Assertion, error) {
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(b64))
	if err != nil {
		// some IdPs use URL encoding
		raw, err = base64.URLEncoding.DecodeString(strings.TrimSpace(b64))
		if err != nil {
			return Assertion{}, fmt.Errorf("saml response b64: %w", err)
		}
	}
	if requireCert && strings.TrimSpace(certPEM) == "" {
		return Assertion{}, fmt.Errorf("saml certificate required")
	}
	// Soft parse: look for NameID and Attribute email
	s := string(raw)
	a := Assertion{SessionID: randomHex(8)}
	if i := strings.Index(s, "<saml:NameID"); i >= 0 {
		if j := strings.Index(s[i:], ">"); j >= 0 {
			rest := s[i+j+1:]
			if k := strings.Index(rest, "</"); k >= 0 {
				a.NameID = strings.TrimSpace(rest[:k])
				a.Email = a.NameID
			}
		}
	}
	if a.Email == "" {
		// AttributeStatement email
		for _, key := range []string{"email", "mail", "Email"} {
			if i := strings.Index(strings.ToLower(s), strings.ToLower(key)); i >= 0 {
				// crude extract next >value<
				if j := strings.Index(s[i:], ">"); j >= 0 {
					rest := s[i+j+1:]
					if k := strings.Index(rest, "<"); k > 0 {
						val := strings.TrimSpace(rest[:k])
						if strings.Contains(val, "@") {
							a.Email = val
							break
						}
					}
				}
			}
		}
	}
	if a.Email == "" {
		// last resort: any email-like token
		return Assertion{}, fmt.Errorf("saml: no NameID/email in assertion")
	}
	a.NotOnOrAfter = time.Now().Add(8 * time.Hour)
	_ = xml.Header // keep encoding/xml imported for future strict parse
	return a, nil
}

func randomHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	const hex = "0123456789abcdef"
	out := make([]byte, n*2)
	for i, v := range b {
		out[i*2] = hex[v>>4]
		out[i*2+1] = hex[v&0xf]
	}
	return string(out)
}
