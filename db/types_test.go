package db

import "testing"

func TestDBBoolScan(t *testing.T) {
	tests := []struct {
		name  string
		value any
		want  bool
	}{
		{name: "bit false", value: []byte{0}, want: false},
		{name: "bit true", value: []byte{1}, want: true},
		{name: "string false", value: "0", want: false},
		{name: "string true", value: "1", want: true},
		{name: "bool true", value: true, want: true},
		{name: "int false", value: int64(0), want: false},
		{name: "int true", value: int64(1), want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got DBBool
			if err := got.Scan(tt.value); err != nil {
				t.Fatalf("Scan() error = %v", err)
			}
			if got.Bool() != tt.want {
				t.Fatalf("Scan() = %v, want %v", got.Bool(), tt.want)
			}
		})
	}
}
