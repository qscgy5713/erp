package page

import "testing"

func TestParse(t *testing.T) {
	cases := []struct {
		page, size string
		want       Params
		offset     int32
	}{
		{"", "", Params{1, DefaultSize}, 0},
		{"3", "10", Params{3, 10}, 20},
		{"0", "-5", Params{1, DefaultSize}, 0},
		{"abc", "1000", Params{1, MaxSize}, 0},
	}
	for _, tc := range cases {
		got := Parse(tc.page, tc.size)
		if got != tc.want || got.Offset() != tc.offset {
			t.Errorf("Parse(%q,%q) = %+v offset %d, want %+v offset %d", tc.page, tc.size, got, got.Offset(), tc.want, tc.offset)
		}
	}
}
