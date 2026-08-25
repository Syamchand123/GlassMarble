package sample

type Service struct {
	Name string
}

func (s *Service) Execute() bool {
	return true
}
