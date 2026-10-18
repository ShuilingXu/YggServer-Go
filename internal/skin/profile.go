package skin

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha1"
	"encoding/base64"
	"encoding/json"
	"fmt"

	"github.com/ShuilingXu/YggServer-Go/internal/model"
)

type Property struct {
	Name      string `json:"name"`
	Value     string `json:"value"`
	Signature string `json:"signature,omitempty"`
}
type ProfileResponse struct {
	model.Profile
	Properties []Property `json:"properties"`
}

func (s *Service) Profile(ctx context.Context, uuid, publicURL string, key *rsa.PrivateKey, signed bool) (ProfileResponse, error) {
	a, err := s.Store.Appearance(ctx, uuid)
	if err != nil {
		return ProfileResponse{}, err
	}
	textures := map[string]any{}
	if a.SkinHash != "" {
		skin := map[string]any{"url": publicURL + "/textures/" + a.SkinHash}
		if a.Kind == model.TextureAlex {
			skin["metadata"] = map[string]string{"model": "slim"}
		}
		textures["SKIN"] = skin
	}
	if a.CapeHash != "" {
		textures["CAPE"] = map[string]string{"url": publicURL + "/textures/" + a.CapeHash}
	}
	payload := map[string]any{"timestamp": s.Now().UTC().UnixMilli(), "profileId": a.Profile.UUID, "profileName": a.Profile.Name, "textures": textures}
	if signed {
		payload["signatureRequired"] = true
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return ProfileResponse{}, fmt.Errorf("encode texture property: %w", err)
	}
	property := Property{Name: "textures", Value: base64.StdEncoding.EncodeToString(data)}
	if signed {
		if key == nil {
			return ProfileResponse{}, fmt.Errorf("signing key unavailable")
		}
		digest := sha1.Sum([]byte(property.Value))
		signature, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA1, digest[:])
		if err != nil {
			return ProfileResponse{}, fmt.Errorf("sign texture property: %w", err)
		}
		property.Signature = base64.StdEncoding.EncodeToString(signature)
	}
	return ProfileResponse{Profile: a.Profile, Properties: []Property{property, {Name: "uploadableTextures", Value: "skin,cape"}}}, nil
}
