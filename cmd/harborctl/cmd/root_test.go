package cmd

import (
	"os/exec"
	"testing"
	"time"
)

func TestCheckJQAvailable(t *testing.T) {
	tests := []struct {
		name string
		want bool
	}{
		{
			name: "check jq availability",
			want: func() bool {
				_, err := exec.LookPath("jq")
				return err == nil
			}(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := checkJQAvailable()
			if got != tt.want {
				t.Errorf("checkJQAvailable() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestFormatWithJQ(t *testing.T) {
	tests := []struct {
		name     string
		jsonData []byte
		wantErr  bool
		skipTest bool
	}{
		{
			name:     "valid json",
			jsonData: []byte(`{"key":"value","number":42}`),
			wantErr:  false,
			skipTest: !checkJQAvailable(),
		},
		{
			name:     "invalid json",
			jsonData: []byte(`{"key":"value",}`),
			wantErr:  true,
			skipTest: !checkJQAvailable(),
		},
		{
			name:     "empty json object",
			jsonData: []byte(`{}`),
			wantErr:  false,
			skipTest: !checkJQAvailable(),
		},
		{
			name:     "json array",
			jsonData: []byte(`[1,2,3]`),
			wantErr:  false,
			skipTest: !checkJQAvailable(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.skipTest {
				t.Skip("jq not available, skipping test")
			}

			got, err := formatWithJQ(tt.jsonData)
			if (err != nil) != tt.wantErr {
				t.Errorf("formatWithJQ() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && got == "" {
				t.Errorf("formatWithJQ() returned empty string for valid JSON")
			}
		})
	}
}

func TestParseJSON(t *testing.T) {
	tests := []struct {
		name    string
		jsonStr string
		wantErr bool
	}{
		{
			name:    "valid simple json",
			jsonStr: `{"key":"value","number":42}`,
			wantErr: false,
		},
		{
			name:    "valid nested json",
			jsonStr: `{"user":{"id":123,"name":"John"},"active":true}`,
			wantErr: false,
		},
		{
			name:    "empty json object",
			jsonStr: `{}`,
			wantErr: false,
		},
		{
			name:    "invalid json - missing quotes",
			jsonStr: `{key:value}`,
			wantErr: true,
		},
		{
			name:    "invalid json - trailing comma",
			jsonStr: `{"key":"value",}`,
			wantErr: true,
		},
		{
			name:    "invalid json - malformed",
			jsonStr: `{"key":"value"`,
			wantErr: true,
		},
		{
			name:    "empty string",
			jsonStr: ``,
			wantErr: true,
		},
		{
			name:    "null value",
			jsonStr: `{"key":null}`,
			wantErr: false,
		},
		{
			name:    "array values",
			jsonStr: `{"items":[1,2,3],"tags":["a","b"]}`,
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseJSON(tt.jsonStr)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseJSON() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr {
				if got == nil {
					t.Errorf("parseJSON() returned nil for valid JSON")
				}
				// Basic validation that we got a valid struct
				if got.GetFields() == nil {
					t.Errorf("parseJSON() returned invalid *structpb.Struct")
				}
			}
		})
	}
}

func TestParseTimestamp(t *testing.T) {
	tests := []struct {
		name     string
		timeStr  string
		wantErr  bool
		wantNil  bool
		expected time.Time
	}{
		{
			name:     "valid RFC3339 timestamp",
			timeStr:  "2023-12-25T15:30:45Z",
			wantErr:  false,
			wantNil:  false,
			expected: time.Date(2023, 12, 25, 15, 30, 45, 0, time.UTC),
		},
		{
			name:     "valid RFC3339 with timezone",
			timeStr:  "2023-12-25T15:30:45-05:00",
			wantErr:  false,
			wantNil:  false,
			expected: time.Date(2023, 12, 25, 20, 30, 45, 0, time.UTC),
		},
		{
			name:     "valid RFC3339 with microseconds",
			timeStr:  "2023-12-25T15:30:45.123456Z",
			wantErr:  false,
			wantNil:  false,
			expected: time.Date(2023, 12, 25, 15, 30, 45, 123456000, time.UTC),
		},
		{
			name:    "empty string",
			timeStr: "",
			wantErr: false,
			wantNil: true,
		},
		{
			name:    "invalid format - missing timezone",
			timeStr: "2023-12-25T15:30:45",
			wantErr: true,
			wantNil: false,
		},
		{
			name:    "invalid format - wrong date format",
			timeStr: "12/25/2023 15:30:45",
			wantErr: true,
			wantNil: false,
		},
		{
			name:    "invalid format - malformed",
			timeStr: "not-a-timestamp",
			wantErr: true,
			wantNil: false,
		},
		{
			name:    "invalid date values",
			timeStr: "2023-13-35T25:70:70Z",
			wantErr: true,
			wantNil: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseTimestamp(tt.timeStr)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseTimestamp() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantNil {
				if got != nil {
					t.Errorf("parseTimestamp() expected nil for empty string, got %v", got)
				}
				return
			}
			if !tt.wantErr {
				if got == nil {
					t.Errorf("parseTimestamp() returned nil for valid timestamp")
					return
				}
				gotTime := got.AsTime()
				if !gotTime.Equal(tt.expected) {
					t.Errorf("parseTimestamp() = %v, want %v", gotTime, tt.expected)
				}
				// Verify it's a valid timestamppb.Timestamp
				if !got.IsValid() {
					t.Errorf("parseTimestamp() returned invalid *timestamppb.Timestamp")
				}
			}
		})
	}
}

func TestParseInt32(t *testing.T) {
	tests := []struct {
		name    string
		s       string
		want    int32
		wantErr bool
	}{
		{
			name:    "valid positive integer",
			s:       "42",
			want:    42,
			wantErr: false,
		},
		{
			name:    "valid negative integer",
			s:       "-123",
			want:    -123,
			wantErr: false,
		},
		{
			name:    "valid zero",
			s:       "0",
			want:    0,
			wantErr: false,
		},
		{
			name:    "empty string",
			s:       "",
			want:    0,
			wantErr: false,
		},
		{
			name:    "valid max int32",
			s:       "2147483647",
			want:    2147483647,
			wantErr: false,
		},
		{
			name:    "valid min int32",
			s:       "-2147483648",
			want:    -2147483648,
			wantErr: false,
		},
		{
			name:    "invalid - not a number",
			s:       "abc",
			want:    0,
			wantErr: true,
		},
		{
			name:    "invalid - decimal number",
			s:       "42.5",
			want:    0,
			wantErr: true,
		},
		{
			name:    "invalid - too large for int32",
			s:       "9223372036854775807",
			want:    0,
			wantErr: true,
		},
		{
			name:    "invalid - too small for int32",
			s:       "-9223372036854775808",
			want:    0,
			wantErr: true,
		},
		{
			name:    "invalid - contains spaces",
			s:       " 42 ",
			want:    0,
			wantErr: true,
		},
		{
			name:    "invalid - hex format",
			s:       "0x2A",
			want:    0,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseInt32(tt.s)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseInt32() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("parseInt32() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestPrintOutput(t *testing.T) {
	tests := []struct {
		name        string
		v           interface{}
		outputJSON  bool
		prettyJSON  bool
		expectPanic bool
	}{
		{
			name:       "simple string - human readable",
			v:          "hello world",
			outputJSON: false,
			prettyJSON: false,
		},
		{
			name:       "simple map - json format",
			v:          map[string]interface{}{"key": "value", "number": 42},
			outputJSON: true,
			prettyJSON: false,
		},
		{
			name:       "simple map - pretty json format",
			v:          map[string]interface{}{"key": "value", "number": 42},
			outputJSON: true,
			prettyJSON: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Capture original values
			origOutputJSON := outputJSON
			origPrettyJSON := prettyJSON

			// Set test values
			outputJSON = tt.outputJSON
			prettyJSON = tt.prettyJSON

			// Restore original values after test
			defer func() {
				outputJSON = origOutputJSON
				prettyJSON = origPrettyJSON
			}()

			// This test mainly ensures printOutput doesn't panic
			// Full output testing would require more complex stdout capture
			defer func() {
				if r := recover(); r != nil && !tt.expectPanic {
					t.Errorf("printOutput() panicked unexpectedly: %v", r)
				}
			}()

			printOutput(tt.v)

			// Basic validation that function completed without panic
			if tt.expectPanic {
				t.Errorf("printOutput() expected to panic but didn't")
			}
		})
	}
}
