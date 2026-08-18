package chat

import "testing"

func TestSplitMessageName(t *testing.T) {
	cases := []struct {
		in            string
		sid, tid, mid string
		ok            bool
	}{
		{"spaces/AAQAlS_sfCg/messages/qeQhBvDA5Os.yr7GQKDADuw", "AAQAlS_sfCg", "qeQhBvDA5Os", "yr7GQKDADuw", true},
		{"spaces/AAQAlS_sfCg/messages/qeQhBvDA5Os.qeQhBvDA5Os", "AAQAlS_sfCg", "qeQhBvDA5Os", "qeQhBvDA5Os", true},
		{"spaces/AAQA/messages/no-dot", "", "", "", false},
		{"spaces/AAQA/threads/qeQh", "", "", "", false},
		{"", "", "", "", false},
	}
	for _, c := range cases {
		sid, tid, mid, ok := splitMessageName(c.in)
		if sid != c.sid || tid != c.tid || mid != c.mid || ok != c.ok {
			t.Errorf("splitMessageName(%q) = (%q,%q,%q,%v), want (%q,%q,%q,%v)",
				c.in, sid, tid, mid, ok, c.sid, c.tid, c.mid, c.ok)
		}
	}
}

func TestSplitSpaceAndThreadName(t *testing.T) {
	if sid, ok := splitSpaceName("spaces/AAQAbc123XyZ"); sid != "AAQAbc123XyZ" || !ok {
		t.Errorf("splitSpaceName = (%q,%v)", sid, ok)
	}
	if _, ok := splitSpaceName("users/123"); ok {
		t.Error("splitSpaceName accepted a non-space name")
	}
	sid, tid, ok := splitThreadName("spaces/AAQAbc123XyZ/threads/kR3nP")
	if sid != "AAQAbc123XyZ" || tid != "kR3nP" || !ok {
		t.Errorf("splitThreadName = (%q,%q,%v)", sid, tid, ok)
	}
	if _, _, ok := splitThreadName("spaces/AAQAbc123XyZ"); ok {
		t.Error("splitThreadName accepted a space name")
	}
}

func TestIsThreadHead(t *testing.T) {
	cases := []struct {
		in        string
		wantHead  bool
		wantKnown bool
	}{
		{"spaces/S/messages/kR3nP.kR3nP", true, true},
		{"spaces/S/messages/kR3nP.zzzz", false, true},
		{"spaces/S/messages/nodot", false, false},
		{"", false, false},
	}
	for _, c := range cases {
		head, known := isThreadHead(c.in)
		if head != c.wantHead || known != c.wantKnown {
			t.Errorf("isThreadHead(%q) = (%v,%v), want (%v,%v)", c.in, head, known, c.wantHead, c.wantKnown)
		}
	}
}

func TestMessageAndThreadLink(t *testing.T) {
	const msg = "spaces/AAQAlS_sfCg/messages/qeQhBvDA5Os.yr7GQKDADuw"
	if got, want := messageLink(msg), "https://chat.google.com/room/AAQAlS_sfCg/qeQhBvDA5Os/yr7GQKDADuw"; got != want {
		t.Errorf("messageLink = %q, want %q", got, want)
	}
	if got := messageLink("spaces/S/messages/nodot"); got != "" {
		t.Errorf("messageLink on unparseable name = %q, want empty", got)
	}
	if got, want := threadLink("spaces/AAQAlS_sfCg/threads/qeQhBvDA5Os"), "https://chat.google.com/room/AAQAlS_sfCg/qeQhBvDA5Os/qeQhBvDA5Os"; got != want {
		t.Errorf("threadLink = %q, want %q", got, want)
	}
	if got := threadLink("nonsense"); got != "" {
		t.Errorf("threadLink on unparseable name = %q, want empty", got)
	}
}

func TestSpaceLink(t *testing.T) {
	cases := []struct {
		name, uri, idx, want string
	}{
		{"spaces/AAQAU1dQC8k", "", "", "https://chat.google.com/u/0/app/chat/AAQAU1dQC8k"},
		{"spaces/AAQAU1dQC8k", "", "1", "https://chat.google.com/u/1/app/chat/AAQAU1dQC8k"},
		// A recognisable spaceUri from the API wins over anything we build.
		{"spaces/AAQAU1dQC8k", "https://chat.google.com/room/AAQAU1dQC8k", "1", "https://chat.google.com/room/AAQAU1dQC8k"},
		// An unrecognisable spaceUri is ignored.
		{"spaces/AAQAU1dQC8k", "https://evil.example/x", "0", "https://chat.google.com/u/0/app/chat/AAQAU1dQC8k"},
		{"garbage", "", "0", ""},
	}
	for _, c := range cases {
		if got := spaceLink(c.name, c.uri, c.idx); got != c.want {
			t.Errorf("spaceLink(%q,%q,%q) = %q, want %q", c.name, c.uri, c.idx, got, c.want)
		}
	}
}

func TestShortID(t *testing.T) {
	if got := shortID("spaces/AAQA/threads/kR3nP"); got != "kR3nP" {
		t.Errorf("shortID = %q", got)
	}
	if got := shortID("kR3nP"); got != "kR3nP" {
		t.Errorf("shortID = %q", got)
	}
	if got := shortID(""); got != "" {
		t.Errorf("shortID = %q", got)
	}
}

func TestFormatMessageID(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{
			in:   "spaces/AAAA9GOspFY/messages/t-uT1uhCWAg.emH4eHFJkeY",
			want: "AAAA9GOspFY/t-uT1uhCWAg/emH4eHFJkeY",
		},
		{
			in:   "spaces/A/messages/t1.t1",
			want: "A/t1/t1",
		},
		{
			in:   "invalid-name",
			want: "invalid-name",
		},
		{
			in:   "",
			want: "",
		},
	}
	for _, c := range cases {
		if got := formatMessageID(c.in); got != c.want {
			t.Errorf("formatMessageID(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestQualifyMessage(t *testing.T) {
	cases := []struct {
		name      string
		ref       string
		space     string
		want      string
		wantError bool
	}{
		{
			name: "3-segment ID",
			ref:  "AAAA9GOspFY/t-uT1uhCWAg/emH4eHFJkeY",
			want: "spaces/AAAA9GOspFY/messages/t-uT1uhCWAg.emH4eHFJkeY",
		},
		{
			name: "chat.google.com web room URL",
			ref:  "https://chat.google.com/room/AAAA9GOspFY/t-uT1uhCWAg/emH4eHFJkeY",
			want: "spaces/AAAA9GOspFY/messages/t-uT1uhCWAg.emH4eHFJkeY",
		},
		{
			name: "chat.google.com web u/0 URL",
			ref:  "https://chat.google.com/u/0/room/AAAA9GOspFY/t-uT1uhCWAg/emH4eHFJkeY",
			want: "spaces/AAAA9GOspFY/messages/t-uT1uhCWAg.emH4eHFJkeY",
		},
		{
			name: "canonical resource name",
			ref:  "spaces/AAAA9GOspFY/messages/t-uT1uhCWAg.emH4eHFJkeY",
			want: "spaces/AAAA9GOspFY/messages/t-uT1uhCWAg.emH4eHFJkeY",
		},
		{
			name:  "short dotted ID with space flag",
			ref:   "t-uT1uhCWAg.emH4eHFJkeY",
			space: "AAAA9GOspFY",
			want:  "spaces/AAAA9GOspFY/messages/t-uT1uhCWAg.emH4eHFJkeY",
		},
		{
			name:  "short slashed ID with spaces/ prefix in space flag",
			ref:   "t-uT1uhCWAg/emH4eHFJkeY",
			space: "spaces/AAAA9GOspFY",
			want:  "spaces/AAAA9GOspFY/messages/t-uT1uhCWAg.emH4eHFJkeY",
		},
		{
			name:      "short ID without space flag",
			ref:       "t-uT1uhCWAg.emH4eHFJkeY",
			wantError: true,
		},
		{
			name:      "empty ref",
			ref:       "",
			wantError: true,
		},
		{
			name:      "invalid garbage ref",
			ref:       "not/a/valid/message/reference/path",
			wantError: true,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := qualifyMessage(c.ref, c.space)
			if c.wantError {
				if err == nil {
					t.Fatalf("qualifyMessage(%q, %q) succeeded, want error", c.ref, c.space)
				}
				return
			}
			if err != nil {
				t.Fatalf("qualifyMessage(%q, %q) error: %v", c.ref, c.space, err)
			}
			if got != c.want {
				t.Errorf("qualifyMessage(%q, %q) = %q, want %q", c.ref, c.space, got, c.want)
			}
		})
	}
}

