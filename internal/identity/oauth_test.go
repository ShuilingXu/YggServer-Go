package identity

import "testing"

func TestOAuthRedirectAndScope(t *testing.T) {
	for _, test := range []struct {
		uri   string
		valid bool
	}{{"https://app.example/callback", true}, {"http://127.0.0.1:1234/callback", true}, {"http://app.example/callback", false}, {"https://app.example/callback#x", false}, {"https://user@app.example", false}, {"javascript:alert(1)", false}} {
		if got := validRedirect(test.uri); got != test.valid {
			t.Fatalf("redirect %q: got %v, want %v", test.uri, got, test.valid)
		}
	}
	scope, err := parseScope("User.Read Player.Read")
	if err != nil || scope != OAuthUserRead|OAuthPlayerRead || scope.String() != "User.Read Player.Read" {
		t.Fatal("scope bits do not retain protocol meaning")
	}
	for _, raw := range []string{"User.Read admin", ""} {
		if _, err := parseScope(raw); err == nil {
			t.Fatal("unknown or empty scope accepted")
		}
	}
}
