package site

import "testing"

func TestEmailPolicy(t *testing.T) {
	c := Defaults()
	c.EmailMode = 1
	c.EmailDomains = "example.org example.com"
	for _, test := range []struct {
		email   string
		allowed bool
	}{{"a@EXAMPLE.org", true}, {"a@sub.example.org", false}, {"a@evilexample.org", false}, {"a@example.net", false}} {
		if got := c.AllowsEmail(test.email); got != test.allowed {
			t.Fatalf("email %q: allowed=%v, want %v", test.email, got, test.allowed)
		}
	}
	c.EmailMode = 2
	if c.AllowsEmail("a@example.org") || !c.AllowsEmail("a@other.net") {
		t.Fatal("deny list not enforced")
	}
}
func TestRejectUnusableSettings(t *testing.T) {
	for _, change := range []func(*Settings){func(c *Settings) { c.SignMin = 101 }, func(c *Settings) { c.SignHours = 0 }, func(c *Settings) { c.MaxPlayers = 0 }, func(c *Settings) { c.EmailMode = 1 }, func(c *Settings) { c.SingleSession = 2 }} {
		c := Defaults()
		change(&c)
		if c.Validate() == nil {
			t.Fatalf("invalid settings accepted: %+v", c)
		}
	}
}

func TestTextureCharge(t *testing.T) {
	c := Defaults()
	c.UploadCost = 2
	c.PrivateUploadCost = 10
	if c.TextureCost(1024, 1) != 2 || c.TextureCost(1025, 1) != 4 || c.TextureCost(1025, 0) != 20 {
		t.Fatal("texture charges must round encoded bytes up to KiB")
	}
}

func TestOIDCConfiguration(t *testing.T) {
	c := Defaults()
	c.OIDCEnabled = 1
	if c.Validate() == nil {
		t.Fatal("enabled OIDC requires issuer, client and display name")
	}
	c.OIDCIssuer = "http://provider.example.org"
	c.OIDCClientID = "client"
	c.OIDCName = "登录"
	if c.Validate() == nil {
		t.Fatal("remote plaintext OIDC accepted")
	}
	c.OIDCIssuer = "https://provider.example.org/tenant"
	if err := c.Validate(); err != nil {
		t.Fatalf("valid OIDC rejected: %v", err)
	}
	c.OIDCIssuer = "https://user:secret@provider.example.org"
	if c.Validate() == nil {
		t.Fatal("issuer with credentials accepted")
	}
}
