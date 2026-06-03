package api

import "testing"

func TestRefererFor(t *testing.T) {
	cases := []struct {
		host string
		want string
	}{
		{"img.evbuc.com", "https://www.eventbrite.com/"},
		{"www.eventbrite.com", "https://www.eventbrite.com/"},
		{"sk-static.com", "https://www.songkick.com/"},
		{"www.songkick.com", "https://www.songkick.com/"},
		{"images.lumacdn.com", "https://lu.ma/"},
		{"lu.ma", "https://lu.ma/"},
		{"s1.ticketm.net", "https://www.ticketmaster.com/"},
		{"www.ticketmaster.com", "https://www.ticketmaster.com/"},
		{"some.other.cdn", "https://some.other.cdn/"},
		{"MIXED.Case.Eventbrite.com", "https://www.eventbrite.com/"},
	}
	for _, c := range cases {
		if got := refererFor(c.host); got != c.want {
			t.Errorf("refererFor(%q) = %q, want %q", c.host, got, c.want)
		}
	}
}
