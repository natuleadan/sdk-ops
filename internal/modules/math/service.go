package math

// Service orchestrates validated addition and history persistence.
type Service struct {
	repo Repository
}

// NewService constructs a Service backed by a Repository.
func NewService(repo Repository) *Service {
	return &Service{repo: repo}
}

// Add validates, sums, and records an addition request.
func (s *Service) Add(req AddRequest) (AddResponse, error) {
	if err := ValidateAddRequest(req); err != nil {
		return AddResponse{}, err
	}
	sum := req.A + req.B
	s.repo.Save(CalculationEntry{A: req.A, B: req.B, Sum: sum})
	return AddResponse{Sum: sum, HistoryCount: s.repo.Count()}, nil
}
