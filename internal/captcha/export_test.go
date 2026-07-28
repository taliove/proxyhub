package captcha

// peekAnswer exposes the stored answer for assertions. Test-only: the answer
// must never be reachable from production code paths.
func (s *Service) peekAnswer(id string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.challenges[id].answer
}
