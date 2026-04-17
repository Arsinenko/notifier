package permissions

type Permission string

const (
	CreatePermission Permission = "CREATE"
	UpdatePermission Permission = "UPDATE"
	DeletePermission Permission = "DELETE"
)
