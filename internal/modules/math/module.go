package math

// Module wires the math domain layers together.
type Module struct {
	Controller *Controller
}

// NewModule assembles InMemoryRepository -> Service -> Controller.
func NewModule() *Module {
	repo := NewInMemoryRepository()
	service := NewService(repo)
	controller := NewController(service)
	return &Module{Controller: controller}
}
