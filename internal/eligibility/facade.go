package eligibility

import (
	"github.com/justwasm/pigasus/internal/features/auth/eligibility"
)

const HTTPStatus = eligibility.HTTPStatus

type Error = eligibility.Error

var (
	AsError         = eligibility.AsError
	ClassifyError   = eligibility.ClassifyError
	RenderNoticeRow = eligibility.RenderNoticeRow
)
