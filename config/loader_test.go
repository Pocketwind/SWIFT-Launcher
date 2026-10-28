package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadPartnersDefaultsArchiveAndPreservesFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "partners.json")
	data := `[
		{
			"name":"MT Input", "description":"existing description", "status":true,
			"direction":"in", "type":"fin", "input_path":"input", "progress_path":"progress",
			"output_path":"output", "ack_path":"ack", "error_path":"error", "extension":".fin",
			"route":{"sender":"TESTKRSE","receiver":"TESTUSAA","message_type":"fin.103"},
			"is_dfa":true, "network_info":{"requestor":"o=testkrse,o=swift","service_code":"swift.generic.fast!p"}
		},
		{"name":"disabled", "archive_enabled":false, "archive_path":"./custom/archive"},
		{"name":"enabled", "archive_enabled":true}
	]`
	if err := os.WriteFile(path, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	partners, err := LoadPartners(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(partners) != 3 {
		t.Fatalf("loaded %d partners, want 3", len(partners))
	}
	if !partners[0].ArchiveEnabled || partners[1].ArchiveEnabled || !partners[2].ArchiveEnabled {
		t.Fatalf("archive default or explicit flags changed: %v, %v, %v", partners[0].ArchiveEnabled, partners[1].ArchiveEnabled, partners[2].ArchiveEnabled)
	}
	if partners[1].ArchivePath != "./custom/archive" || partners[1].ArchiveDirectory() != "./custom/archive" {
		t.Fatalf("custom archive path lost: %+v", partners[1])
	}
	p := partners[0]
	if p.Name != "MT Input" || p.Description != "existing description" || !p.Status || p.Direction != "in" || p.Type != "fin" || p.Extension != ".fin" {
		t.Fatalf("existing partner identity changed: %+v", p)
	}
	if p.InputPath != "input" || p.ProgressPath != "progress" || p.OutputPath != "output" || p.AckPath != "ack" || p.ErrorPath != "error" {
		t.Fatalf("existing partner paths changed: %+v", p)
	}
	if p.Route.Sender != "TESTKRSE" || p.Route.Receiver != "TESTUSAA" || p.Route.MessageType != "fin.103" || !p.IsDFA || p.DFAInfo.Requestor != "o=testkrse,o=swift" || p.DFAInfo.ServiceCode != "swift.generic.fast!p" {
		t.Fatalf("existing nested routing or network fields changed: %+v", p)
	}
}

func TestPartnerArchiveDirectory(t *testing.T) {
	progress := filepath.Join("MESG", "MT", "progress")
	archive := filepath.Join("MESG", "MT", "archive")
	for _, tc := range []struct {
		name    string
		partner Partner
		want    string
	}{
		{"empty progress", Partner{}, "./archive"},
		{"blank progress", Partner{ProgressPath: "  "}, "./archive"},
		{"sibling", Partner{ProgressPath: progress}, archive},
		{"trailing separator", Partner{ProgressPath: progress + string(filepath.Separator)}, archive},
		{"blank override", Partner{ProgressPath: progress, ArchivePath: "  "}, archive},
		{"custom directory", Partner{ProgressPath: progress, ArchivePath: "./custom/archive"}, "./custom/archive"},
		{"trimmed custom directory", Partner{ArchivePath: " ./custom/archive "}, "./custom/archive"},
		{"custom without progress", Partner{ArchivePath: "./custom/archive"}, "./custom/archive"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.partner.ArchiveDirectory(); got != tc.want {
				t.Fatalf("archive directory = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestStatusDataIncludesDisabledArchiveFlag(t *testing.T) {
	data, err := json.Marshal(StatusData{ArchiveEnabled: false})
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		t.Fatal(err)
	}
	if string(fields["archive_enabled"]) != "false" {
		t.Fatalf("disabled archive flag omitted from status: %s", data)
	}
	if _, exists := fields["archive_path"]; exists {
		t.Fatalf("empty archive path should be omitted: %s", data)
	}
}

func TestLoadPartnersRejectsInvalidArchiveFlag(t *testing.T) {
	path := filepath.Join(t.TempDir(), "partners.json")
	if err := os.WriteFile(path, []byte(`[{"name":"invalid","archive_enabled":"false"}]`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadPartners(path); err == nil {
		t.Fatal("invalid archive flag accepted")
	}
}
