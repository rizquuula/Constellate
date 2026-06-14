package terminal

// Session holds the domain state of a single terminal session.
// All fields are unexported; use accessors and mutators.
type Session struct {
	id     string
	shell  string
	cwd    string
	cols   int
	rows   int
	pid    int
	status Status
}

// New constructs a Session in the running state.
func New(id, shell, cwd string, cols, rows int) Session {
	return Session{
		id:     id,
		shell:  shell,
		cwd:    cwd,
		cols:   cols,
		rows:   rows,
		status: StatusRunning,
	}
}

func (s Session) ID() string     { return s.id }
func (s Session) Shell() string  { return s.shell }
func (s Session) Cwd() string    { return s.cwd }
func (s Session) Cols() int      { return s.cols }
func (s Session) Rows() int      { return s.rows }
func (s Session) Pid() int       { return s.pid }
func (s Session) Status() Status { return s.status }

// SetPid sets the OS process ID of the session's shell.
func (s *Session) SetPid(pid int) { s.pid = pid }

// SetStatus updates the session's lifecycle status.
func (s *Session) SetStatus(status Status) { s.status = status }

// SetSize updates the terminal dimensions.
func (s *Session) SetSize(cols, rows int) {
	s.cols = cols
	s.rows = rows
}
