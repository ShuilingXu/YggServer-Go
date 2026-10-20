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

type UUIDStrategy int16

const (
	UUIDOffline   UUIDStrategy = 0
	UUIDRandom    UUIDStrategy = 1
	UUIDPreserved UUIDStrategy = 2
)

type RecoveryKind int16

const (
	RecoveryVerify RecoveryKind = 0
	RecoveryReset  RecoveryKind = 1
)

type TextureState int16

const (
	TextureActive TextureState = 0
	TextureHidden TextureState = 1
)

type ReportState int16

const (
	ReportPending  ReportState = 0
	ReportResolved ReportState = 1
	ReportRejected ReportState = 2
)

type AuditAction int16

const (
	AuditUserManage      AuditAction = 0
	AuditOwnerGrant      AuditAction = 1
	AuditTextureModerate AuditAction = 2
	AuditReportResolve   AuditAction = 3
	AuditSiteSettings    AuditAction = 4
	AuditOAuthClient     AuditAction = 5
	AuditExternalBind    AuditAction = 6
	AuditExternalUnbind  AuditAction = 7
	AuditSetup           AuditAction = 8
	AuditBSImport        AuditAction = 9
)

type AuditResult int16

const (
	AuditApplied   AuditResult = 0
	AuditDismissed AuditResult = 1
	AuditUnchanged int16       = -1
)
