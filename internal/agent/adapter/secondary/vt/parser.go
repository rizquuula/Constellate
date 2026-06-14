package vt

// parser implements the Paul Williams ANSI escape-sequence state machine.
// State persists between Write calls so partial sequences split across two
// calls are handled correctly.
//
// References:
//   - https://vt100.net/emu/dec_ansi_parser  (Williams state machine)
//   - ECMA-48 §5 (C0, C1, control sequences)
//   - XTerm source (for OSC/DCS boundary bytes)

// parserState enumerates the parser's state machine states.
type parserState uint8

const (
	stGround        parserState = iota // normal character processing
	stEscape                           // after ESC
	stEscapeInter                      // ESC + intermediate byte(s)
	stCSIEntry                         // after ESC [
	stCSIParam                         // inside CSI parameter digits
	stCSIInter                         // CSI + intermediate byte(s) before final
	stCSIIgnore                        // invalid CSI sequence → discard until final
	stOSCString                        // inside OSC string (ESC ] … BEL/ST)
	stDCSEntry                         // after ESC P
	stDCSPassthrough                   // inside DCS string (until ST)
)

// actionKind identifies what action the parser has completed.
type actionKind uint8

const (
	actNone    actionKind = iota
	actPrint              // printable character
	actExecute            // C0 control byte
	actESC                // completed ESC sequence (final, intermediate)
	actCSI                // completed CSI sequence
	actOSC                // completed OSC string (ignored)
	actDCS                // completed DCS string (ignored)
)

// action is the result of feeding one byte to the parser.
type action struct {
	kind         actionKind
	ch           byte   // for actPrint / actExecute
	final        byte   // for actESC / actCSI
	intermediate []byte // for actESC / actCSI
	params       []int  // for actCSI (parsed; 0 means "omitted")
}

// parser holds the mutable state machine state.
type parser struct {
	state       parserState
	inter       [4]byte // intermediate bytes (ESC and CSI share these)
	nInter      int
	paramBuf    [32]byte // raw CSI parameter bytes
	nParam      int
	oscBuf      [512]byte // OSC string accumulator (discarded but must be consumed)
	nOSC        int
}

func (p *parser) init() {
	p.state = stGround
}

// feedByte feeds a single byte into the parser and returns an action (which
// may be actNone if the sequence is incomplete) and the parser's new state
// (for reference; callers generally don't need it).
func (p *parser) feedByte(b byte) (action, parserState) {
	// C0 controls (0x00–0x1F) are executed in most states except
	// OSC/DCS accumulators (where they terminate).
	if b < 0x20 {
		return p.handleC0(b)
	}

	switch p.state {
	case stGround:
		// Printable ASCII 0x20–0x7E.
		if b >= 0x20 && b <= 0x7E {
			return action{kind: actPrint, ch: b}, stGround
		}
		if b == 0x7F { // DEL — ignore
			return action{}, stGround
		}
		return action{}, stGround

	case stEscape:
		switch {
		case b == '[': // CSI introducer
			p.resetCSI()
			p.state = stCSIEntry
			return action{}, stCSIEntry
		case b == ']': // OSC string
			p.nOSC = 0
			p.state = stOSCString
			return action{}, stOSCString
		case b == 'P': // DCS
			p.state = stDCSEntry
			return action{}, stDCSEntry
		case b >= 0x20 && b <= 0x2F: // intermediate byte
			p.inter[p.nInter] = b
			if p.nInter < len(p.inter)-1 {
				p.nInter++
			}
			p.state = stEscapeInter
			return action{}, stEscapeInter
		case b >= 0x30 && b <= 0x7E: // final byte
			a := p.buildESC(b)
			p.state = stGround
			return a, stGround
		case b == 0x7F: // DEL — ignore
			return action{}, stEscape
		default:
			p.state = stGround
			return action{}, stGround
		}

	case stEscapeInter:
		switch {
		case b >= 0x20 && b <= 0x2F: // more intermediate bytes
			if p.nInter < len(p.inter)-1 {
				p.inter[p.nInter] = b
				p.nInter++
			}
			return action{}, stEscapeInter
		case b >= 0x30 && b <= 0x7E: // final byte
			a := p.buildESC(b)
			p.state = stGround
			return a, stGround
		default:
			p.state = stGround
			return action{}, stGround
		}

	case stCSIEntry:
		switch {
		case b >= '0' && b <= '9': // parameter digit
			p.paramBuf[p.nParam] = b
			p.nParam++
			p.state = stCSIParam
			return action{}, stCSIParam
		case b == ';': // parameter separator (empty first param)
			if p.nParam < len(p.paramBuf)-1 {
				p.paramBuf[p.nParam] = ';'
				p.nParam++
			}
			p.state = stCSIParam
			return action{}, stCSIParam
		case b >= 0x20 && b <= 0x2F: // intermediate
			if p.nInter < len(p.inter)-1 {
				p.inter[p.nInter] = b
				p.nInter++
			}
			p.state = stCSIInter
			return action{}, stCSIInter
		case b == '?', b == '<', b == '=', b == '>': // private marker — store as intermediate
			if p.nInter < len(p.inter)-1 {
				p.inter[p.nInter] = b
				p.nInter++
			}
			p.state = stCSIParam
			return action{}, stCSIParam
		case b >= 0x40 && b <= 0x7E: // final byte with no params
			a := p.buildCSI(b)
			p.state = stGround
			return a, stGround
		case b == 0x7F:
			return action{}, stCSIEntry
		default:
			p.state = stCSIIgnore
			return action{}, stCSIIgnore
		}

	case stCSIParam:
		switch {
		case b >= '0' && b <= '9':
			if p.nParam < len(p.paramBuf)-1 {
				p.paramBuf[p.nParam] = b
				p.nParam++
			}
			return action{}, stCSIParam
		case b == ';':
			if p.nParam < len(p.paramBuf)-1 {
				p.paramBuf[p.nParam] = ';'
				p.nParam++
			}
			return action{}, stCSIParam
		case b == ':': // sub-parameter separator (treat like ';' for simplicity)
			if p.nParam < len(p.paramBuf)-1 {
				p.paramBuf[p.nParam] = ';'
				p.nParam++
			}
			return action{}, stCSIParam
		case b >= 0x20 && b <= 0x2F:
			if p.nInter < len(p.inter)-1 {
				p.inter[p.nInter] = b
				p.nInter++
			}
			p.state = stCSIInter
			return action{}, stCSIInter
		case b >= 0x40 && b <= 0x7E:
			a := p.buildCSI(b)
			p.state = stGround
			return a, stGround
		case b == 0x7F:
			return action{}, stCSIParam
		default:
			p.state = stCSIIgnore
			return action{}, stCSIIgnore
		}

	case stCSIInter:
		switch {
		case b >= 0x20 && b <= 0x2F:
			if p.nInter < len(p.inter)-1 {
				p.inter[p.nInter] = b
				p.nInter++
			}
			return action{}, stCSIInter
		case b >= 0x40 && b <= 0x7E:
			a := p.buildCSI(b)
			p.state = stGround
			return a, stGround
		default:
			p.state = stCSIIgnore
			return action{}, stCSIIgnore
		}

	case stCSIIgnore:
		if b >= 0x40 && b <= 0x7E {
			p.state = stGround
		}
		return action{}, p.state

	case stOSCString:
		switch b {
		case 0x07: // BEL terminates OSC
			p.state = stGround
			return action{kind: actOSC}, stGround
		case 0x5C: // if preceded by ESC this is ST; handled via C0 ESC path
			// Bare 0x5C inside OSC just accumulates.
		}
		if p.nOSC < len(p.oscBuf)-1 {
			p.oscBuf[p.nOSC] = b
			p.nOSC++
		}
		return action{}, stOSCString

	case stDCSEntry:
		if b == 0x5C { // ST (bare — real ST is ESC \)
			p.state = stGround
			return action{kind: actDCS}, stGround
		}
		p.state = stDCSPassthrough
		return action{}, stDCSPassthrough

	case stDCSPassthrough:
		if b == 0x5C {
			p.state = stGround
			return action{kind: actDCS}, stGround
		}
		return action{}, stDCSPassthrough
	}

	return action{}, p.state
}

// handleC0 processes a C0 control byte (0x00–0x1F) in the current state.
func (p *parser) handleC0(b byte) (action, parserState) {
	// BEL terminates an OSC string regardless of other logic.
	if b == 0x07 && p.state == stOSCString {
		p.state = stGround
		return action{kind: actOSC}, stGround
	}
	// BEL inside DCS passthrough: discard and keep accumulating.
	if b == 0x07 && p.state == stDCSPassthrough {
		return action{}, p.state
	}

	switch b {
	case 0x1B: // ESC — begin escape sequence (also used as ST introducer inside OSC/DCS)
		// When inside an OSC/DCS string, ESC transitions to stEscape so the next
		// byte ('\\' = ST) can terminate the string gracefully.
		p.nInter = 0
		p.state = stEscape
		return action{}, stEscape
	case 0x18, 0x1A: // CAN, SUB — cancel sequence
		p.state = stGround
		return action{}, stGround
	case 0x9C: // ST (8-bit) — terminate OSC/DCS
		if p.state == stOSCString {
			p.state = stGround
			return action{kind: actOSC}, stGround
		}
		if p.state == stDCSPassthrough {
			p.state = stGround
			return action{kind: actDCS}, stGround
		}
		p.state = stGround
		return action{}, stGround
	}

	// Inside OSC/DCS accumulation states, swallow non-terminating C0 bytes.
	if p.state == stOSCString || p.state == stDCSPassthrough || p.state == stDCSEntry {
		return action{}, p.state
	}

	// Other C0 controls: execute if in ground/CSI param states, ignore elsewhere.
	switch p.state {
	case stGround, stCSIEntry, stCSIParam, stCSIInter, stCSIIgnore, stEscape, stEscapeInter:
		if b == '\r' || b == '\n' || b == '\t' || b == '\b' || b == '\a' || b == '\v' || b == '\f' {
			return action{kind: actExecute, ch: b}, p.state
		}
	}
	return action{}, p.state
}

func (p *parser) resetCSI() {
	p.nInter = 0
	p.nParam = 0
}

func (p *parser) buildESC(final byte) action {
	inter := make([]byte, p.nInter)
	copy(inter, p.inter[:p.nInter])

	// ESC \ is ST — if we're terminating an OSC that was interrupted by ESC:
	// just return actOSC (the caller's state machine will handle it).
	if final == 0x5C {
		p.nInter = 0
		return action{kind: actOSC}
	}

	p.nInter = 0
	return action{kind: actESC, final: final, intermediate: inter}
}

func (p *parser) buildCSI(final byte) action {
	inter := make([]byte, p.nInter)
	copy(inter, p.inter[:p.nInter])
	params := parseParams(p.paramBuf[:p.nParam])
	p.resetCSI()
	return action{kind: actCSI, final: final, intermediate: inter, params: params}
}

// parseParams converts a raw CSI parameter byte string into a []int.
// Each field is separated by ';'. An omitted field (empty string) is 0.
func parseParams(raw []byte) []int {
	if len(raw) == 0 {
		return nil
	}
	// Count separators to pre-size.
	n := 1
	for _, b := range raw {
		if b == ';' {
			n++
		}
	}
	params := make([]int, 0, n)
	cur := 0
	for _, b := range raw {
		if b == ';' {
			params = append(params, cur)
			cur = 0
		} else if b >= '0' && b <= '9' {
			cur = cur*10 + int(b-'0')
		}
	}
	params = append(params, cur)
	return params
}
