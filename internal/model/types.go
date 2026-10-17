package model

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
