package math

// Controller adapts transport inputs to Service calls.
type Controller struct {
	service *Service
}

// NewController constructs a Controller from a configured Service.
func NewController(service *Service) *Controller {
	return &Controller{service: service}
}

// HandleAdd executes an addition request through Service.
func (c *Controller) HandleAdd(a int64, b int64) (AddResponse, error) {
	return c.service.Add(AddRequest{A: a, B: b})
}
