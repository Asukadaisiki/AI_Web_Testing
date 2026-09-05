package corrections

import "testing"

func TestGeneralizeURL(t *testing.T) {
	got, err := generalizeURL("https://example.com/orders/123?token=abc123def456ghi7&view=full")
	if err != nil {
		t.Fatalf("generalizeURL() error = %v", err)
	}
	want := "https://example.com/orders/*?token=%2A&view=full"
	if got != want {
		t.Fatalf("generalizeURL() = %q, want %q", got, want)
	}
}

func TestGeneralizeURLRejectsRelativeURL(t *testing.T) {
	if _, err := generalizeURL("/orders/123"); err == nil {
		t.Fatal("generalizeURL() error = nil")
	}
}
