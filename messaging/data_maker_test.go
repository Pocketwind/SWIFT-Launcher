package messaging

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Pocketwind/SWIFT-Launcher/logging"
)

func TestMXDataMakerRejectsMissingBody(t *testing.T) {
	path := filepath.Join(t.TempDir(), "incomplete.xml")
	if err := os.WriteFile(path, []byte("<DataPDU><Header/></DataPDU>"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := MXDataMaker(path, make(chan logging.LogData, 2)); err == nil {
		t.Fatal("missing Body accepted")
	}
}
