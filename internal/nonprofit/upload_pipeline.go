package nonprofit

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
)

type AssetKind string

const (
	DonorReceipt      AssetKind = "donor_receipt"
	VolunteerReminder AssetKind = "volunteer_reminder"
	CampaignReport    AssetKind = "campaign_report"
)

var safeReference = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

type Upload struct {
	Reference string
	Kind      AssetKind
	Filename  string
	Image     []byte
}

type Record struct {
	Reference       string    `json:"reference"`
	Kind            AssetKind `json:"kind"`
	Format          string    `json:"format"`
	ObjectKey       string    `json:"object_key"`
	State           string    `json:"state"`
	ReportingStream string    `json:"reporting_stream"`
}

type MediaStore interface {
	Convert(context.Context, string, string, []byte, string) ([]byte, error)
	PutObject(context.Context, string, string, string, []byte, string) error
}

type Pipeline struct {
	Store  MediaStore
	Bucket string
}

func Plan(kind AssetKind) (format, stream string, err error) {
	switch kind {
	case DonorReceipt:
		return "webp", "donor_operations", nil
	case VolunteerReminder:
		return "webp", "volunteer_operations", nil
	case CampaignReport:
		return "avif", "campaign_reporting", nil
	default:
		return "", "", errors.New("kind must be donor_receipt, volunteer_reminder, or campaign_report")
	}
}

func (p Pipeline) Process(ctx context.Context, upload Upload) (Record, error) {
	if !safeReference.MatchString(upload.Reference) || upload.Filename == "" || len(upload.Image) == 0 {
		return Record{}, errors.New("reference must be path-safe; filename and image are required")
	}
	format, stream, err := Plan(upload.Kind)
	if err != nil {
		return Record{}, err
	}
	base := strings.TrimSuffix(filepath.Base(upload.Filename), filepath.Ext(upload.Filename))
	if base == "" || base == "." {
		return Record{}, errors.New("filename must have a basename")
	}
	key := fmt.Sprintf("%s/%s/%s.%s", upload.Kind, upload.Reference, base, format)
	operation := stableOperation(upload.Kind, upload.Reference, key)
	converted, err := p.Store.Convert(ctx, upload.Filename, format, upload.Image, operation+":convert")
	if err != nil {
		return Record{}, fmt.Errorf("convert nonprofit upload: %w", err)
	}
	if err := p.Store.PutObject(ctx, p.Bucket, key, "image/"+format, converted, operation+":store"); err != nil {
		return Record{}, fmt.Errorf("store nonprofit upload: %w", err)
	}
	return Record{
		Reference: upload.Reference, Kind: upload.Kind, Format: format, ObjectKey: key,
		State: "stored", ReportingStream: stream,
	}, nil
}

func stableOperation(kind AssetKind, reference, key string) string {
	sum := sha256.Sum256([]byte(string(kind) + "\x00" + reference + "\x00" + key))
	return hex.EncodeToString(sum[:16])
}
