package report

import (
	"reflect"
	"strings"
	"testing"
)

func TestParseSelection(t *testing.T) {
	cases := []struct {
		name    string
		input   string
		n       int
		want    []int
		errPart string
	}{
		{name: "all", input: "all", n: 3, want: []int{1, 2, 3}},
		{name: "all uppercase padded", input: "  ALL ", n: 2, want: []int{1, 2}},
		{name: "all with n zero", input: "all", n: 0, want: []int{}},
		{name: "none", input: "none", n: 3, want: []int{}},
		{name: "none mixed case", input: "None", n: 3, want: []int{}},
		{name: "empty", input: "", n: 3, want: []int{}},
		{name: "blank", input: "   ", n: 3, want: []int{}},
		{name: "list and range", input: "1,3-5", n: 5, want: []int{1, 3, 4, 5}},
		{name: "whitespace tolerated", input: " 1 , 3 - 5 ", n: 5, want: []int{1, 3, 4, 5}},
		{name: "sorted and deduplicated", input: "5,1,3-5,1", n: 5, want: []int{1, 3, 4, 5}},
		{name: "single element range", input: "2-2", n: 3, want: []int{2}},
		{name: "out of range high", input: "1,9", n: 3, errPart: "9"},
		{name: "zero out of range", input: "0", n: 3, errPart: "0"},
		{name: "range end out of range", input: "2-7", n: 3, errPart: "2-7"},
		{name: "reversed range", input: "5-3", n: 5, errPart: "5-3"},
		{name: "garbage", input: "1,foo", n: 3, errPart: "foo"},
		{name: "empty token", input: "1,,2", n: 3, errPart: "empty"},
		{name: "half range", input: "3-", n: 5, errPart: "3-"},
		{name: "negative", input: "-2", n: 5, errPart: "-2"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseSelection(tc.input, tc.n)
			if tc.errPart != "" {
				if err == nil {
					t.Fatalf("ParseSelection(%q, %d) = %v, want error", tc.input, tc.n, got)
				}
				if !strings.Contains(err.Error(), tc.errPart) {
					t.Fatalf("error %q does not name %q", err, tc.errPart)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
}
