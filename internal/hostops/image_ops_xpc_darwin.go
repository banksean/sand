//go:build darwin && cgo

package hostops

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/banksean/sand/internal/applecontainer/types"
	"github.com/banksean/sand/internal/applecontainer/xpc"
	"golang.org/x/term"
)

type xpcImageOps struct {
	client *xpc.Client
}

func NewXPCImageOps() (ImageOps, error) {
	client, err := xpc.NewClient(xpc.WithService(xpc.ImageServiceIdentifier))
	if err != nil {
		return nil, err
	}
	return &xpcImageOps{client: client}, nil
}

func (o *xpcImageOps) List(ctx context.Context) ([]types.ImageEntry, error) {
	images, err := o.client.ListImages(ctx)
	if err != nil {
		return nil, err
	}
	ret := make([]types.ImageEntry, 0, len(images))
	for _, image := range images {
		ret = append(ret, imageDescriptionToEntry(image))
	}
	return ret, nil
}

func (o *xpcImageOps) Pull(ctx context.Context, image string, w io.Writer) (func() error, error) {
	renderer := newXPCPullProgressRenderer(w)
	_, err := o.client.PullImage(ctx, image, xpc.ImagePullOptions{}, renderer.Handle)
	renderer.Finish()
	if err != nil {
		return nil, err
	}
	return func() error { return nil }, nil
}

func (o *xpcImageOps) Inspect(ctx context.Context, name string) ([]*types.ImageManifest, error) {
	image, err := o.findImage(ctx, name)
	if err != nil {
		return nil, err
	}
	manifest, err := o.inspectImage(ctx, image)
	if err != nil {
		return nil, err
	}
	return []*types.ImageManifest{manifest}, nil
}

func (o *xpcImageOps) findImage(ctx context.Context, name string) (xpc.ImageDescription, error) {
	images, err := o.client.ListImages(ctx)
	if err != nil {
		return xpc.ImageDescription{}, err
	}
	for _, image := range images {
		if image.Reference == name {
			return image, nil
		}
	}
	return xpc.ImageDescription{}, fmt.Errorf("image %q not found", name)
}

func (o *xpcImageOps) inspectImage(ctx context.Context, image xpc.ImageDescription) (*types.ImageManifest, error) {
	content, err := o.content(ctx, image.Descriptor.Digest)
	if err != nil {
		return nil, err
	}
	index := types.Index{
		Size:        int(image.Descriptor.Size),
		Digest:      image.Descriptor.Digest,
		MediaType:   image.Descriptor.MediaType,
		Annotations: image.Descriptor.Annotations,
	}
	if isIndexMediaType(image.Descriptor.MediaType) {
		var parsed ociIndex
		if err := json.Unmarshal(content, &parsed); err != nil {
			return nil, fmt.Errorf("decode image index %q: %w", image.Reference, err)
		}
		if index.MediaType == "" {
			index.MediaType = parsed.MediaType
		}
		if index.Annotations == nil {
			index.Annotations = parsed.Annotations
		}
		var variants []types.ImageVariant
		for _, desc := range parsed.Manifests {
			if skipManifestDescriptor(desc) {
				continue
			}
			variant, err := o.variant(ctx, desc, desc.Platform)
			if err != nil {
				return nil, err
			}
			variants = append(variants, variant)
		}
		return &types.ImageManifest{Name: image.Reference, Index: index, Variants: variants}, nil
	}
	platform := image.Descriptor.Platform
	variant, err := o.variantFromManifestData(ctx, image.Descriptor, platform, content)
	if err != nil {
		return nil, err
	}
	return &types.ImageManifest{Name: image.Reference, Index: index, Variants: []types.ImageVariant{variant}}, nil
}

func (o *xpcImageOps) variant(ctx context.Context, desc xpc.Descriptor, platform *xpc.Platform) (types.ImageVariant, error) {
	content, err := o.content(ctx, desc.Digest)
	if err != nil {
		return types.ImageVariant{}, err
	}
	return o.variantFromManifestData(ctx, desc, platform, content)
}

func (o *xpcImageOps) variantFromManifestData(ctx context.Context, desc xpc.Descriptor, platform *xpc.Platform, content []byte) (types.ImageVariant, error) {
	var manifest ociManifest
	if err := json.Unmarshal(content, &manifest); err != nil {
		return types.ImageVariant{}, fmt.Errorf("decode image manifest %q: %w", desc.Digest, err)
	}
	configContent, err := o.content(ctx, manifest.Config.Digest)
	if err != nil {
		return types.ImageVariant{}, err
	}
	var config xpc.OCIImage
	if err := json.Unmarshal(configContent, &config); err != nil {
		return types.ImageVariant{}, fmt.Errorf("decode image config %q: %w", manifest.Config.Digest, err)
	}
	if platform == nil {
		platform = &xpc.Platform{OS: config.OS, Architecture: config.Architecture}
		if config.Variant != nil {
			platform.Variant = *config.Variant
		}
	}
	return types.ImageVariant{
		Size:     int(desc.Size),
		Platform: platformToTypes(*platform),
		Config:   ociConfigToTypes(config),
	}, nil
}

func (o *xpcImageOps) content(ctx context.Context, digest string) ([]byte, error) {
	path, err := o.client.GetContentPath(ctx, digest)
	if err != nil {
		return nil, err
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read content %q at %s: %w", digest, path, err)
	}
	return content, nil
}

type ociIndex struct {
	MediaType   string            `json:"mediaType"`
	Manifests   []xpc.Descriptor  `json:"manifests"`
	Annotations map[string]string `json:"annotations,omitempty"`
}

type ociManifest struct {
	MediaType string           `json:"mediaType"`
	Config    xpc.Descriptor   `json:"config"`
	Layers    []xpc.Descriptor `json:"layers"`
}

func imageDescriptionToEntry(image xpc.ImageDescription) types.ImageEntry {
	return types.ImageEntry{
		ID: imageID(image.Descriptor.Digest),
		Configuration: types.ImageConfiguration{
			Name: image.Reference,
			Descriptor: types.ImageDescriptor{
				Size:        int(image.Descriptor.Size),
				Digest:      image.Descriptor.Digest,
				MediaType:   image.Descriptor.MediaType,
				Annotations: image.Descriptor.Annotations,
			},
		},
	}
}

func imageID(digest string) string {
	if _, suffix, ok := strings.Cut(digest, ":"); ok {
		return suffix
	}
	return digest
}

func isIndexMediaType(mediaType string) bool {
	return strings.Contains(mediaType, "image.index") || strings.Contains(mediaType, "manifest.list")
}

func skipManifestDescriptor(desc xpc.Descriptor) bool {
	if desc.Platform == nil {
		return true
	}
	if desc.Annotations != nil && desc.Annotations["vnd.docker.reference.type"] == "attestation-manifest" {
		return true
	}
	return false
}

func platformToTypes(platform xpc.Platform) types.Platform {
	return types.Platform{OS: platform.OS, Architecture: platform.Architecture, Variant: platform.Variant}
}

func ociConfigToTypes(image xpc.OCIImage) types.ImageVariantConfig {
	var created time.Time
	if image.Created != nil {
		created, _ = time.Parse(time.RFC3339Nano, *image.Created)
	}
	var cfg types.ImageVariantContainerConfig
	if image.Config != nil {
		cfg.Cmd = append([]string{}, image.Config.Cmd...)
		cfg.Env = append([]string{}, image.Config.Env...)
		cfg.Labels = image.Config.Labels
		if image.Config.WorkingDir != nil {
			cfg.WorkingDir = *image.Config.WorkingDir
		}
	}
	return types.ImageVariantConfig{
		Config:       cfg,
		Rootfs:       types.Rootfs{Type: image.Rootfs.Type, DiffIDs: append([]string{}, image.Rootfs.DiffIDs...)},
		History:      historyToTypes(image.History),
		Architecture: image.Architecture,
		Created:      created,
		OS:           image.OS,
	}
}

func historyToTypes(history []xpc.History) []types.HistoryEntry {
	ret := make([]types.HistoryEntry, 0, len(history))
	for _, item := range history {
		var created time.Time
		if item.Created != nil {
			created, _ = time.Parse(time.RFC3339Nano, *item.Created)
		}
		entry := types.HistoryEntry{Created: created}
		if item.CreatedBy != nil {
			entry.CreatedBy = *item.CreatedBy
		}
		if item.Comment != nil {
			entry.Comment = *item.Comment
		}
		if item.EmptyLayer != nil {
			entry.EmptyLayer = *item.EmptyLayer
		}
		ret = append(ret, entry)
	}
	return ret
}

type xpcPullProgressRenderer struct {
	mu          sync.Mutex
	w           io.Writer
	terminal    bool
	description string
	sub         string
	itemsName   string
	tasks       int64
	totalTasks  int64
	items       int64
	totalItems  int64
	size        int64
	totalSize   int64
	last        string
}

func newXPCPullProgressRenderer(w io.Writer) *xpcPullProgressRenderer {
	if w == nil {
		w = io.Discard
	}
	renderer := &xpcPullProgressRenderer{w: w}
	// TODO: fix this, because the "is a terminal" determination can only be done
	// by the sand CLI, which makes the gRPC call to the daemon, which calls this.
	// The gRPC call should probably return a stream of structured responses, it *it* should
	// determine whether or not to print control characters.
	if file, ok := w.(*os.File); ok && term.IsTerminal(int(file.Fd())) {
		renderer.terminal = true
	}
	return renderer
}

func (r *xpcPullProgressRenderer) Handle(update xpc.ProgressUpdate) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if update.Description != nil {
		r.description = *update.Description
	}
	if update.SubDescription != nil {
		r.sub = *update.SubDescription
	}
	if update.ItemsName != nil {
		r.itemsName = *update.ItemsName
	}
	applyCounter(&r.tasks, update.AddTasks, update.SetTasks)
	applyCounter(&r.totalTasks, update.AddTotalTasks, update.SetTotalTasks)
	applyCounter(&r.items, update.AddItems, update.SetItems)
	applyCounter(&r.totalItems, update.AddTotalItems, update.SetTotalItems)
	applyCounter(&r.size, update.AddSize, update.SetSize)
	applyCounter(&r.totalSize, update.AddTotalSize, update.SetTotalSize)
	r.renderLocked(false)
}

func (r *xpcPullProgressRenderer) Finish() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.renderLocked(true)
}

func (r *xpcPullProgressRenderer) renderLocked(final bool) {
	line := r.line()
	if line == "" {
		return
	}
	if line == r.last {
		if final && r.terminal {
			fmt.Fprintln(r.w)
		}
		return
	}
	if r.terminal {
		fmt.Fprintf(r.w, "\r\033[2K%s", line)
		if final {
			fmt.Fprintln(r.w)
		}
	} else {
		fmt.Fprintln(r.w, line)
	}
	r.last = line
}

func (r *xpcPullProgressRenderer) line() string {
	parts := make([]string, 0, 5)
	if r.description != "" {
		parts = append(parts, r.description)
	}
	if r.sub != "" {
		parts = append(parts, r.sub)
	}
	if r.totalTasks > 0 {
		parts = append(parts, fmt.Sprintf("tasks %d/%d", r.tasks, r.totalTasks))
	}
	if r.totalItems > 0 {
		name := r.itemsName
		if name == "" {
			name = "items"
		}
		parts = append(parts, fmt.Sprintf("%s %d/%d", name, r.items, r.totalItems))
	}
	if r.totalSize > 0 {
		parts = append(parts, fmt.Sprintf("%s/%s", formatBytes(r.size), formatBytes(r.totalSize)))
	}
	return strings.Join(parts, " ")
}

func applyCounter(value *int64, add, set *int64) {
	if set != nil {
		*value = *set
	}
	if add != nil {
		*value += *add
	}
}

func formatBytes(value int64) string {
	const unit = 1024
	if value < unit {
		return fmt.Sprintf("%dB", value)
	}
	div, exp := int64(unit), 0
	for n := value / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f%ciB", float64(value)/float64(div), "KMGTPE"[exp])
}
