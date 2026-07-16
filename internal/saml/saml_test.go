package saml

import (
	"encoding/base64"
	"strings"
	"testing"
)

func TestMetadataAndParse(t *testing.T) {
	c := Config{EntityID: "https://sp/trace", ACSURL: "https://sp/saml/acs", IDPSSOURL: "https://idp/sso"}
	md := c.MetadataXML()
	if !strings.Contains(md, "SPSSODescriptor") {
		t.Fatal(md)
	}
	u, err := c.RedirectURL("rs")
	if err != nil || !strings.Contains(u, "SAMLRequest=") {
		t.Fatal(u, err)
	}
	raw := `<samlp:Response xmlns:samlp="urn:oasis:names:tc:SAML:2.0:protocol" xmlns:saml="urn:oasis:names:tc:SAML:2.0:assertion">
  <saml:Assertion><saml:Subject><saml:NameID>user@example.com</saml:NameID></saml:Subject></saml:Assertion>
</samlp:Response>`
	a, err := ParseResponse(base64.StdEncoding.EncodeToString([]byte(raw)), false, "")
	if err != nil || a.Email != "user@example.com" {
		t.Fatal(a, err)
	}
}
