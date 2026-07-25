package jobs

// Service is the Wails-bound facade over the registry.  It deliberately
// exposes only the read and control surface — producers get a *Handle
// through the registry instead, so the frontend cannot invent jobs.
type Service struct {
	reg *Registry
}

// NewService wraps a registry for frontend binding.
func NewService(reg *Registry) *Service {
	return &Service{reg: reg}
}

// GetJobs returns every known job — active first by registration order,
// including recently finished ones so the panel can show outcomes.
func (s *Service) GetJobs() []Job {
	return s.reg.Snapshot()
}

// GetJobLog returns the retained log tail for one job.
func (s *Service) GetJobLog(id string) []LogEntry {
	return s.reg.Logs(id)
}

// PauseJob asks the owning subsystem to pause a job.  Returns
// immediately; the job reports "paused" once it actually stops.
func (s *Service) PauseJob(id string) {
	s.reg.Pause(id)
}

// ResumeJob continues a paused job.
func (s *Service) ResumeJob(id string) {
	s.reg.Resume(id)
}

// CancelJob abandons a job.
func (s *Service) CancelJob(id string) {
	s.reg.Cancel(id)
}

// DismissJob removes a single finished job from the list.
func (s *Service) DismissJob(id string) {
	s.reg.Remove(id)
}

// ClearFinishedJobs removes every terminal job from the list.
func (s *Service) ClearFinishedJobs() {
	s.reg.ClearFinished()
}
