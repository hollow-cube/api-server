package transform

import (
	v1 "github.com/hollow-cube/hc-services/services/map/api/v1"
	"github.com/hollow-cube/hc-services/services/map/internal/pkg/model"
)

func Map2API(m *model.Map) *v1.MapData {
	var subVariant *string
	if m.Settings.SubVariant != nil {
		v := string(*m.Settings.SubVariant)
		subVariant = &v
	}
	var objects []*v1.ObjectData
	for _, object := range m.Objects {
		objects = append(objects, &v1.ObjectData{
			Id:   object.Id,
			Type: object.Type,
			Pos: &v1.Point{
				X: float32(object.Pos.X),
				Y: float32(object.Pos.Y),
				Z: float32(object.Pos.Z),
			},
			Data: object.Data,
		})
	}
	tags := make([]*string, len(m.Settings.Tags))
	for i, tag := range m.Settings.Tags {
		var t string = tag // this alias is necessary to copy the value
		tags[i] = &t
	}

	var extra map[string]interface{}
	if m.Settings.Extra != nil {
		extra = m.Settings.Extra
	} else {
		extra = make(map[string]interface{})
	}

	return &v1.MapData{
		Id:           m.Id,
		Owner:        m.Owner,
		CreatedAt:    m.CreatedAt,
		LastModified: m.UpdatedAt,
		Settings: &v1.MapSettings{
			Name:       m.Settings.Name,
			Icon:       m.Settings.Icon,
			Size:       m.Settings.Size,
			Variant:    v1.MapVariant(m.Settings.Variant),
			Subvariant: subVariant,
			SpawnPoint: Pos2API(&m.Settings.SpawnPoint),

			// Gameplay settings
			OnlySprint: m.Settings.OnlySprint,
			NoSprint:   m.Settings.NoSprint,
			NoJump:     m.Settings.NoJump,
			NoSneak:    m.Settings.NoSneak,
			Boat:       m.Settings.Boat,
			Extra:      extra,

			Tags: tags,
		},
		Verification: v1.MapDataVerification(m.Verification),

		PublishedId: m.PublishedId,
		PublishedAt: m.PublishedAt,

		UniquePlays: m.UniquePlays,
		ClearRate:   m.ClearRate,
		Likes:       m.Likes,

		Quality: v1.MapQuality(m.QualityOverride),

		Objects: objects,
	}
}
