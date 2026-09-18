package saml

import (
	"encoding/base64"
	"strings"
	"testing"
	"time"
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
	nb := time.Now().Add(-time.Minute).UTC().Format(time.RFC3339)
	noa := time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
	raw := `<samlp:Response xmlns:samlp="urn:oasis:names:tc:SAML:2.0:protocol" xmlns:saml="urn:oasis:names:tc:SAML:2.0:assertion">
  <saml:Assertion><saml:Subject><saml:NameID>user@example.com</saml:NameID>
  <saml:SubjectConfirmation><saml:SubjectConfirmationData Recipient="https://sp/saml/acs" NotOnOrAfter="` + noa + `"/></saml:SubjectConfirmation></saml:Subject>
  <saml:Conditions NotBefore="` + nb + `" NotOnOrAfter="` + noa + `"><saml:AudienceRestriction><saml:Audience>https://sp/trace</saml:Audience></saml:AudienceRestriction></saml:Conditions>
  </saml:Assertion>
</samlp:Response>`
	a, err := ParseResponse(base64.StdEncoding.EncodeToString([]byte(raw)), VerifyOpts{
		EntityID: "https://sp/trace", ACSURL: "https://sp/saml/acs",
	})
	if err != nil || a.Email != "user@example.com" {
		t.Fatal(a, err)
	}
}

func TestParseRejectsWrongAudience(t *testing.T) {
	noa := time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
	raw := `<samlp:Response xmlns:samlp="urn:oasis:names:tc:SAML:2.0:protocol" xmlns:saml="urn:oasis:names:tc:SAML:2.0:assertion">
  <saml:Assertion><saml:Subject><saml:NameID>evil@example.com</saml:NameID></saml:Subject>
  <saml:Conditions NotOnOrAfter="` + noa + `"><saml:AudienceRestriction><saml:Audience>https://evil/x</saml:Audience></saml:AudienceRestriction></saml:Conditions>
  </saml:Assertion>
</samlp:Response>`
	if _, err := ParseResponse(base64.StdEncoding.EncodeToString([]byte(raw)), VerifyOpts{EntityID: "https://sp/trace"}); err == nil {
		t.Fatal("expected audience mismatch")
	}
}

func TestParseRejectsExpired(t *testing.T) {
	past := time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)
	raw := `<samlp:Response xmlns:samlp="urn:oasis:names:tc:SAML:2.0:protocol" xmlns:saml="urn:oasis:names:tc:SAML:2.0:assertion">
  <saml:Assertion><saml:Subject><saml:NameID>user@example.com</saml:NameID></saml:Subject>
  <saml:Conditions NotOnOrAfter="` + past + `"></saml:Conditions>
  </saml:Assertion>
</samlp:Response>`
	if _, err := ParseResponse(base64.StdEncoding.EncodeToString([]byte(raw)), VerifyOpts{}); err == nil {
		t.Fatal("expected expiry rejection")
	}
}

func TestParseRejectsGarbage(t *testing.T) {
	if _, err := ParseResponse("!!!not-base64!!!", VerifyOpts{}); err == nil {
		t.Fatal("expected b64 error")
	}
	if _, err := ParseResponse(base64.StdEncoding.EncodeToString([]byte("<xml><oops>")), VerifyOpts{}); err == nil {
		t.Fatal("expected xml error")
	}
}
