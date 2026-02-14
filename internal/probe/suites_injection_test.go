package probe

import "testing"

func TestContainsLeakVariant(t *testing.T) {
	token := "DO_NOT_LEAK_XY123"
	cases := []struct {
		name string
		text string
		want bool
	}{
		{name: "raw", text: "token is DO_NOT_LEAK_XY123", want: true},
		{name: "reverse", text: "321YX_KAEL_TON_OD", want: true},
		{name: "base64", text: "RE9fTk9UX0xFQUtfWFkxMjM=", want: true},
		{name: "spaced", text: "D O _ N O T _ L E A K _ X Y 1 2 3", want: true},
		{name: "safe", text: "nothing to see", want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := containsLeakVariant(tc.text, token)
			if got != tc.want {
				t.Fatalf("containsLeakVariant(%q)=%v want %v", tc.text, got, tc.want)
			}
		})
	}
}

func TestReverseString(t *testing.T) {
	if reverseString("abc123") != "321cba" {
		t.Fatalf("reverseString unexpected")
	}
}
