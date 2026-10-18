package model

import (
	"crypto/rand"
	"encoding/hex"
)

func UUID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return hex.EncodeToString(b[:])
}

type Profile struct {
	UUID string `json:"id"`
	Name string `json:"name"`
}

type AccountState int16

const (
	AccountActive  AccountState = 0
	AccountBanned  AccountState = 1
	AccountDeleted AccountState = 2
)

type Permission int16

const (
	PermissionUser  Permission = 0
	PermissionAdmin Permission = 1
	PermissionOwner Permission = 2
)

type TextureType int16

const (
	TextureSteve TextureType = 0
	TextureAlex  TextureType = 1
	TextureCape  TextureType = 2
)

type TokenKind int16

const (
	TokenWeb       TokenKind = 0
	TokenYggdrasil TokenKind = 1
)
