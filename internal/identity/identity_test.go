package identity

import "testing"

func TestResolveGitHubNoreplyAuthor(t *testing.T) {
	got, err := Resolve("github.com", "github-username", Profile{
		ID:    123456,
		Login: "github-username",
		Name:  "Test User",
	})
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if got.Name != "github-username" || got.Email != "123456+github-username@users.noreply.github.com" {
		t.Fatalf("Resolve() = %+v", got)
	}
}

func TestResolveFallsBackToLoginName(t *testing.T) {
	got, err := Resolve("github.com", "octocat", Profile{ID: 1, Login: "octocat"})
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if got.Name != "octocat" {
		t.Fatalf("name = %q, want octocat", got.Name)
	}
}

func TestResolveRejectsWrongAccount(t *testing.T) {
	_, err := Resolve("github.com", "github-username", Profile{ID: 1, Login: "github-alt-username"})
	if err == nil {
		t.Fatal("Resolve() accepted a token for another account")
	}
}

func TestResolveRejectsMalformedAccountAndProfile(t *testing.T) {
	for name, value := range map[string]struct {
		requested string
		profile   Profile
	}{
		"bad requested login": {requested: "../secret", profile: Profile{ID: 1, Login: "octocat"}},
		"bad returned login":  {requested: "octocat", profile: Profile{ID: 1, Login: "../secret"}},
		"missing user id":     {requested: "octocat", profile: Profile{Login: "octocat"}},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := Resolve("github.com", value.requested, value.profile); err == nil {
				t.Fatal("Resolve() accepted malformed input")
			}
		})
	}
}

func TestAuthorEmailUsesEnterpriseProfileEmail(t *testing.T) {
	got, err := AuthorEmail("github.example.com", Profile{ID: 2, Login: "octocat", Email: "octocat@example.com"})
	if err != nil {
		t.Fatalf("AuthorEmail() error = %v", err)
	}
	if got != "octocat@example.com" {
		t.Fatalf("email = %q", got)
	}
}
