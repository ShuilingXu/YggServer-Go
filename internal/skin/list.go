package skin

import "github.com/ShuilingXu/YggServer-Go/internal/model"

type ListOptions struct {
	Offset, Limit         int
	Query, Uploader, Sort string
	Kind                  *model.TextureType
}
