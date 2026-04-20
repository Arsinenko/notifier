package permissions

type Permission string

const (
	CreatePermission Permission = "CREATE"
	RenamePermission Permission = "RENAME"
	DeletePermission Permission = "REMOVE"
	WritePermission  Permission = "WRITE"
)
