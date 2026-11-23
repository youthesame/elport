package plan

type ApprovedUploadPath struct {
	abs string
}

func (p ApprovedUploadPath) Path() string {
	return p.abs
}

func (p ApprovedUploadPath) Valid() bool {
	return p.abs != ""
}
