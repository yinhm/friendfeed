package media

import (
	"encoding/base64"
	"errors"
	"strings"
	"time"

	"github.com/yinhm/friendfeed/util"
)

const (
	MaxEntryAttachments     = 10
	MaxEntryAttachmentBytes = 100 << 20
)

type StagedObject struct {
	Name      string
	Digest    string
	Extension string
	MimeType  string
	Size      int
	Role      string
}

type StagedImage struct {
	Width, Height                   int
	ThumbnailWidth, ThumbnailHeight int
	Objects                         []StagedObject
}

type StagedAttachment struct {
	DisplayName string
	Object      StagedObject
}

type PublishedImage struct {
	URL, ThumbnailURL string
	Width, Height     int
}

type PublishedAttachment struct {
	URL, Name, MimeType string
	Size                int
}

// UploadPipeline is the shared, authenticated-call-agnostic media boundary.
// Callers own authentication, request concurrency and transport parsing; this
// type owns validation, staging and canonical publication only.
type UploadPipeline struct {
	staging *StagingStore
	baseURL string
}

func NewUploadPipeline(cfg *util.Config) *UploadPipeline {
	return &UploadPipeline{
		staging: NewStagingStore(cfg),
		baseURL: strings.TrimRight(PublicURL(cfg, ""), "/"),
	}
}

func (p *UploadPipeline) StageImage(content []byte, maxWidth int) (*StagedImage, error) {
	if p == nil || p.staging == nil {
		return nil, errors.New("media: upload pipeline is not configured")
	}
	prepared, err := PrepareUploadedImage(content, maxWidth)
	if err != nil {
		return nil, err
	}
	originalName, originalDigest, err := p.staging.Put(prepared.Original, prepared.Extension)
	if err != nil {
		return nil, err
	}
	objects := []StagedObject{{
		Name: originalName, Digest: originalDigest, Extension: prepared.Extension,
		MimeType: prepared.MimeType, Size: len(prepared.Original), Role: "original",
	}}
	if !bytesEqual(prepared.Original, prepared.Thumbnail) {
		name, digest, err := p.staging.Put(prepared.Thumbnail, prepared.ThumbnailExtension)
		if err != nil {
			return nil, err
		}
		objects = append(objects, StagedObject{
			Name: name, Digest: digest, Extension: prepared.ThumbnailExtension,
			MimeType: prepared.ThumbnailMimeType, Size: len(prepared.Thumbnail), Role: "thumbnail",
		})
	}
	return &StagedImage{
		Width: prepared.Width, Height: prepared.Height,
		ThumbnailWidth: prepared.ThumbnailWidth, ThumbnailHeight: prepared.ThumbnailHeight,
		Objects: objects,
	}, nil
}

func bytesEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func (p *UploadPipeline) StageAttachment(name string, content []byte) (*StagedAttachment, error) {
	if p == nil || p.staging == nil {
		return nil, errors.New("media: upload pipeline is not configured")
	}
	info, err := InspectAttachment(name, content)
	if err != nil {
		return nil, err
	}
	objectName, digest, err := p.staging.Put(content, info.Extension)
	if err != nil {
		return nil, err
	}
	return &StagedAttachment{DisplayName: info.Name, Object: StagedObject{
		Name: objectName, Digest: digest, Extension: info.Extension,
		MimeType: info.MimeType, Size: info.Size, Role: "file",
	}}, nil
}

func (p *UploadPipeline) PromoteObject(object StagedObject) (string, error) {
	if p == nil || p.staging == nil || object.Name == "" || object.Digest == "" ||
		object.Extension == "" || object.Size <= 0 || object.Size > MaxUploadFileBytes {
		return "", errors.New("media: invalid staged object")
	}
	return p.staging.Promote(object.Name, object.Digest, object.Extension, object.Size)
}

func (p *UploadPipeline) PromoteImage(image *StagedImage) (*PublishedImage, error) {
	if image == nil || len(image.Objects) == 0 {
		return nil, errors.New("media: staged image is required")
	}
	var original, thumbnail StagedObject
	for _, object := range image.Objects {
		switch object.Role {
		case "original":
			original = object
		case "thumbnail":
			thumbnail = object
		}
	}
	if original.Name == "" {
		return nil, errors.New("media: image original is missing")
	}
	if thumbnail.Name == "" {
		thumbnail = original
	}
	originalKey, err := p.PromoteObject(original)
	if err != nil {
		return nil, err
	}
	thumbnailKey, err := p.PromoteObject(thumbnail)
	if err != nil {
		return nil, err
	}
	return &PublishedImage{
		URL: p.baseURL + "/" + originalKey, ThumbnailURL: p.baseURL + "/" + thumbnailKey,
		Width: image.ThumbnailWidth, Height: image.ThumbnailHeight,
	}, nil
}

func (p *UploadPipeline) PromoteAttachment(attachment *StagedAttachment) (*PublishedAttachment, error) {
	if attachment == nil || attachment.DisplayName == "" {
		return nil, errors.New("media: staged attachment is required")
	}
	key, err := p.PromoteObject(attachment.Object)
	if err != nil {
		return nil, err
	}
	return &PublishedAttachment{
		URL:  p.baseURL + "/" + key + "?download=" + base64.RawURLEncoding.EncodeToString([]byte(attachment.DisplayName)),
		Name: attachment.DisplayName, MimeType: attachment.Object.MimeType, Size: attachment.Object.Size,
	}, nil
}

func (p *UploadPipeline) StagingURL(object StagedObject) string {
	return p.baseURL + "/" + StagingDirectory + "/" + object.Name
}

func (p *UploadPipeline) Cleanup(now time.Time, ttl time.Duration) (int, error) {
	if p == nil || p.staging == nil {
		return 0, nil
	}
	return p.staging.Cleanup(now, ttl)
}
