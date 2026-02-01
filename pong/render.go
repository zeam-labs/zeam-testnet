package pong

import (
	"fmt"
	"strings"
)

const (

	RenderWidth  = 80
	RenderHeight = 24

	CharBall       = "●"
	CharPaddle     = "█"
	CharWallH      = "─"
	CharWallV      = "│"
	CharCornerTL   = "┌"
	CharCornerTR   = "┐"
	CharCornerBL   = "└"
	CharCornerBR   = "┘"
	CharEmpty      = " "
	CharScoreSep   = " : "
)

type Renderer struct {
	width  int
	height int

	scaleX float64
	scaleY float64

	lastFrame string
}

func NewRenderer() *Renderer {
	return &Renderer{
		width:  RenderWidth,
		height: RenderHeight,
		scaleX: float64(RenderWidth-2) / float64(FieldWidth),
		scaleY: float64(RenderHeight-4) / float64(FieldHeight),
	}
}

func (r *Renderer) Render(state *State) string {
	var sb strings.Builder

	sb.WriteString("\033[2J\033[H")

	sb.WriteString(r.renderHeader(state))

	sb.WriteString(CharCornerTL)
	sb.WriteString(strings.Repeat(CharWallH, r.width-2))
	sb.WriteString(CharCornerTR)
	sb.WriteString("\n")

	field := r.renderField(state)
	for _, row := range field {
		sb.WriteString(CharWallV)
		sb.WriteString(row)
		sb.WriteString(CharWallV)
		sb.WriteString("\n")
	}

	sb.WriteString(CharCornerBL)
	sb.WriteString(strings.Repeat(CharWallH, r.width-2))
	sb.WriteString(CharCornerBR)
	sb.WriteString("\n")

	sb.WriteString(r.renderFooter(state))

	r.lastFrame = sb.String()
	return r.lastFrame
}

func (r *Renderer) renderHeader(state *State) string {
	title := "════════════════════ FLOW PONG ════════════════════"
	score := fmt.Sprintf("Player 1: %d%sPlayer 2: %d", state.Score1, CharScoreSep, state.Score2)

	titlePad := (r.width - len(title)) / 2
	scorePad := (r.width - len(score)) / 2

	var sb strings.Builder
	sb.WriteString(strings.Repeat(" ", titlePad))
	sb.WriteString(title)
	sb.WriteString("\n")
	sb.WriteString(strings.Repeat(" ", scorePad))
	sb.WriteString(score)
	sb.WriteString("\n\n")

	return sb.String()
}

func (r *Renderer) renderField(state *State) []string {

	fieldHeight := r.height - 4
	fieldWidth := r.width - 2
	field := make([][]rune, fieldHeight)
	for i := range field {
		field[i] = make([]rune, fieldWidth)
		for j := range field[i] {
			field[i][j] = ' '
		}
	}

	paddle1X := int(float64(PaddleMargin) * r.scaleX)
	paddle1Y := int(float64(state.Paddle1Y) * r.scaleY)
	paddle1H := int(float64(PaddleHeight) * r.scaleY)
	if paddle1H < 1 {
		paddle1H = 1
	}

	paddle2X := int(float64(FieldWidth-PaddleMargin-PaddleWidth) * r.scaleX)
	paddle2Y := int(float64(state.Paddle2Y) * r.scaleY)

	for dy := 0; dy < paddle1H; dy++ {
		y := paddle1Y + dy
		if y >= 0 && y < fieldHeight && paddle1X >= 0 && paddle1X < fieldWidth {
			field[y][paddle1X] = '█'
		}
	}

	for dy := 0; dy < paddle1H; dy++ {
		y := paddle2Y + dy
		if y >= 0 && y < fieldHeight && paddle2X >= 0 && paddle2X < fieldWidth {
			field[y][paddle2X] = '█'
		}
	}

	ballX := int(float64(state.BallX) * r.scaleX)
	ballY := int(float64(state.BallY) * r.scaleY)
	if ballY >= 0 && ballY < fieldHeight && ballX >= 0 && ballX < fieldWidth {
		field[ballY][ballX] = '●'
	}

	rows := make([]string, fieldHeight)
	for i, row := range field {
		rows[i] = string(row)
	}

	return rows
}

func (r *Renderer) renderFooter(state *State) string {
	var sb strings.Builder

	if state.GameOver {
		winner := fmt.Sprintf("═══════════ PLAYER %d WINS! ═══════════", state.Winner)
		pad := (r.width - len(winner)) / 2
		sb.WriteString(strings.Repeat(" ", pad))
		sb.WriteString(winner)
		sb.WriteString("\n")
		sb.WriteString("            Press 'r' to restart, 'q' to quit\n")
	} else {
		info := fmt.Sprintf("Tick: %d │ Flow Active", state.Tick)
		controls := "P1: W/S │ P2: ↑/↓ │ Q: Quit"
		sb.WriteString(info)
		sb.WriteString(strings.Repeat(" ", r.width-len(info)-len(controls)))
		sb.WriteString(controls)
		sb.WriteString("\n")
	}

	return sb.String()
}

type MinimalRenderer struct {
	width  int
	height int
}

func NewMinimalRenderer() *MinimalRenderer {
	return &MinimalRenderer{
		width:  60,
		height: 20,
	}
}

func (r *MinimalRenderer) Render(state *State) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("\n  FLOW PONG - Tick %d\n", state.Tick))
	sb.WriteString(fmt.Sprintf("  Score: %d - %d\n\n", state.Score1, state.Score2))

	scaleX := float64(r.width-4) / float64(FieldWidth)
	scaleY := float64(r.height-2) / float64(FieldHeight)

	sb.WriteString("  +" + strings.Repeat("-", r.width-4) + "+\n")

	for y := 0; y < r.height-2; y++ {
		sb.WriteString("  |")

		for x := 0; x < r.width-4; x++ {

			gameX := int(float64(x) / scaleX)
			gameY := int(float64(y) / scaleY)

			char := ' '

			if gameX >= int(state.BallX) && gameX < int(state.BallX)+BallSize &&
				gameY >= int(state.BallY) && gameY < int(state.BallY)+BallSize {
				char = 'O'
			}

			if gameX >= PaddleMargin && gameX < PaddleMargin+PaddleWidth &&
				gameY >= int(state.Paddle1Y) && gameY < int(state.Paddle1Y)+PaddleHeight {
				char = '#'
			}

			if gameX >= FieldWidth-PaddleMargin-PaddleWidth && gameX < FieldWidth-PaddleMargin &&
				gameY >= int(state.Paddle2Y) && gameY < int(state.Paddle2Y)+PaddleHeight {
				char = '#'
			}

			sb.WriteByte(byte(char))
		}

		sb.WriteString("|\n")
	}

	sb.WriteString("  +" + strings.Repeat("-", r.width-4) + "+\n")

	if state.GameOver {
		sb.WriteString(fmt.Sprintf("\n  PLAYER %d WINS!\n", state.Winner))
	} else {
		sb.WriteString("\n  P1: W/S  P2: I/K  Q: Quit\n")
	}

	return sb.String()
}

type FrameBuffer struct {
	current  string
	previous string
}

func NewFrameBuffer() *FrameBuffer {
	return &FrameBuffer{}
}

func (fb *FrameBuffer) Update(frame string) bool {
	changed := frame != fb.current
	fb.previous = fb.current
	fb.current = frame
	return changed
}

func (fb *FrameBuffer) Current() string {
	return fb.current
}
