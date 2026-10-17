package nonprofit

import (
	"context"
	"reflect"
	"testing"
)

type recordingStore struct {
	format       string
	convertKey   string
	storageWrite []string
}

func (s *recordingStore) Convert(_ context.Context, _ string, format string, _ []byte, operationKey string) ([]byte, error) {
	s.format, s.convertKey = format, operationKey
	return []byte("converted-image"), nil
}

func (s *recordingStore) PutObject(_ context.Context, bucket, key, contentType string, data []byte, operationKey string) error {
	s.storageWrite = []string{bucket, key, contentType, string(data), operationKey}
	return nil
}

func TestProcessRoutesNonprofitAssets(t *testing.T) {
	tests := []struct {
		name       string
		kind       AssetKind
		reference  string
		wantFormat string
		wantKey    string
		wantStream string
	}{
		{"donor receipt favors broad delivery", DonorReceipt, "gift-2048", "webp", "donor_receipt/gift-2048/receipt.webp", "donor_operations"},
		{"volunteer reminder favors broad delivery", VolunteerReminder, "shift-18", "webp", "volunteer_reminder/shift-18/reminder.webp", "volunteer_operations"},
		{"campaign report favors compact archive", CampaignReport, "spring-drive", "avif", "campaign_report/spring-drive/impact.avif", "campaign_reporting"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := &recordingStore{}
			got, err := (Pipeline{Store: store, Bucket: "nonprofit-media"}).Process(context.Background(), Upload{
				Reference: tt.reference, Kind: tt.kind, Filename: "incoming/" + filenameFor(tt.kind), Image: []byte("source-image"),
			})
			if err != nil {
				t.Fatal(err)
			}
			if got.Format != tt.wantFormat || got.ObjectKey != tt.wantKey || got.ReportingStream != tt.wantStream || got.State != "stored" {
				t.Fatalf("record = %+v", got)
			}
			wantWrite := []string{"nonprofit-media", tt.wantKey, "image/" + tt.wantFormat, "converted-image", store.storageWrite[4]}
			if store.format != tt.wantFormat || !reflect.DeepEqual(store.storageWrite, wantWrite) {
				t.Fatalf("boundary format=%q write=%v", store.format, store.storageWrite)
			}
			if store.convertKey == "" || store.convertKey == store.storageWrite[4] {
				t.Fatalf("stage operation keys must be stable and distinct")
			}
		})
	}
}

func filenameFor(kind AssetKind) string {
	switch kind {
	case DonorReceipt:
		return "receipt.png"
	case VolunteerReminder:
		return "reminder.jpg"
	default:
		return "impact.tiff"
	}
}
