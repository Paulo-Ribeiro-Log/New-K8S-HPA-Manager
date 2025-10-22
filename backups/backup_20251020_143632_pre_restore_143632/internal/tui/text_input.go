package tui

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// TextInput representa um campo de entrada de texto com cursor
type TextInput struct {
	Value        string
	CursorPos    int
	MaxLength    int
	Placeholder  string
}

// NewTextInput cria um novo campo de entrada de texto
func NewTextInput(initialValue string) *TextInput {
	return &TextInput{
		Value:     initialValue,
		CursorPos: len([]rune(initialValue)), // Cursor no final
		MaxLength: 0, // 0 = sem limite
	}
}

// HandleKeyPress processa eventos de teclado para edição de texto
// Retorna: (novoValor, novaPosicaoCursor, continuarEditando, comandoTea)
func (ti *TextInput) HandleKeyPress(msg tea.KeyMsg, onSave func(string), onCancel func()) (string, int, bool, tea.Cmd) {
	runes := []rune(ti.Value)
	cursorPos := ti.CursorPos

	// Garantir que a posição do cursor está dentro dos limites
	if cursorPos > len(runes) {
		cursorPos = len(runes)
	}
	if cursorPos < 0 {
		cursorPos = 0
	}

	switch msg.String() {
	case "enter":
		if onSave != nil {
			onSave(ti.Value)
		}
		return "", 0, false, nil // Sair do modo de edição

	case "esc":
		if onCancel != nil {
			onCancel()
		}
		return "", 0, false, nil // Sair do modo de edição

	case "left", "ctrl+b":
		if cursorPos > 0 {
			cursorPos--
		}

	case "right", "ctrl+f":
		if cursorPos < len(runes) {
			cursorPos++
		}

	case "home", "ctrl+a":
		cursorPos = 0

	case "end", "ctrl+e":
		cursorPos = len(runes)

	case "backspace", "ctrl+h":
		if cursorPos > 0 && len(runes) > 0 {
			// Remove o caractere à esquerda do cursor
			newRunes := append(runes[:cursorPos-1], runes[cursorPos:]...)
			ti.Value = string(newRunes)
			cursorPos--
		}

	case "delete", "ctrl+d":
		if cursorPos < len(runes) {
			// Remove o caractere à direita do cursor
			newRunes := append(runes[:cursorPos], runes[cursorPos+1:]...)
			ti.Value = string(newRunes)
		}

	case "ctrl+u": // Apagar do cursor até o início
		if cursorPos > 0 {
			ti.Value = string(runes[cursorPos:])
			cursorPos = 0
		}

	case "ctrl+k": // Apagar do cursor até o final
		if cursorPos < len(runes) {
			ti.Value = string(runes[:cursorPos])
		}

	default:
		// Inserir caracteres normais
		if len(msg.String()) == 1 {
			char := []rune(msg.String())[0]
			// Verificar se é um caractere válido (não control)
			if char >= 32 && char < 127 || char > 127 {
				// Verificar limite de comprimento
				if ti.MaxLength == 0 || len(runes) < ti.MaxLength {
					// Inserir caractere na posição do cursor
					newRunes := append(runes[:cursorPos], append([]rune{char}, runes[cursorPos:]...)...)
					ti.Value = string(newRunes)
					cursorPos++
				}
			}
		}
	}

	// Atualizar estado
	ti.CursorPos = cursorPos
	return ti.Value, ti.CursorPos, true, nil // Continuar editando
}

// RenderWithCursor renderiza o texto com cursor visual que mostra o caractere sobreposto
func (ti *TextInput) RenderWithCursor() string {
	runes := []rune(ti.Value)
	cursorPos := ti.CursorPos

	// Garantir que a posição do cursor está dentro dos limites
	if cursorPos > len(runes) {
		cursorPos = len(runes)
	}
	if cursorPos < 0 {
		cursorPos = 0
	}

	// Se o cursor está no final do texto, inserir o cursor normal
	if cursorPos >= len(runes) {
		if len(runes) == 0 && ti.Placeholder != "" {
			return "█" // Cursor sobre placeholder vazio
		}
		return string(runes) + "█"
	}

	// Se o cursor está no meio do texto, mostrar o caractere sendo sobreposto
	// com destaque visual usando Lipgloss
	char := runes[cursorPos]

	// Criar estilo de cursor com cores inversas
	cursorStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#000000")). // Texto preto
		Background(lipgloss.Color("#FFFF00")). // Fundo amarelo
		Bold(true)

	// Tratar diferentes tipos de caracteres
	var highlightedChar string
	if char == ' ' {
		// Para espaços, mostrar um caractere visível
		highlightedChar = cursorStyle.Render("·") // Ponto médio para representar espaço
	} else {
		// Para outros caracteres, mostrar com destaque
		highlightedChar = cursorStyle.Render(string(char))
	}

	result := string(runes[:cursorPos]) + highlightedChar + string(runes[cursorPos+1:])
	return result
}

// RenderWithSimpleCursor renderiza com cursor simples (fallback para terminais que não suportam cores)
func (ti *TextInput) RenderWithSimpleCursor() string {
	runes := []rune(ti.Value)
	cursorPos := ti.CursorPos

	// Garantir que a posição do cursor está dentro dos limites
	if cursorPos > len(runes) {
		cursorPos = len(runes)
	}
	if cursorPos < 0 {
		cursorPos = 0
	}

	// Se o cursor está no final do texto, inserir o cursor normal
	if cursorPos >= len(runes) {
		return string(runes) + "█"
	}

	// Se o cursor está no meio do texto, mostrar o caractere com delimitadores
	char := runes[cursorPos]

	var highlightedChar string
	if char == ' ' {
		highlightedChar = "▓" // Bloco para espaços
	} else {
		highlightedChar = "[" + string(char) + "]" // Delimitadores para outros caracteres
	}

	result := string(runes[:cursorPos]) + highlightedChar + string(runes[cursorPos+1:])
	return result
}

// SetValue define o valor do texto e ajusta o cursor
func (ti *TextInput) SetValue(value string) {
	ti.Value = value
	ti.CursorPos = len([]rune(value)) // Cursor no final
}

// SetCursorPosition define a posição do cursor
func (ti *TextInput) SetCursorPosition(pos int) {
	runes := []rune(ti.Value)
	if pos > len(runes) {
		pos = len(runes)
	}
	if pos < 0 {
		pos = 0
	}
	ti.CursorPos = pos
}

// Clear limpa o texto e reseta o cursor
func (ti *TextInput) Clear() {
	ti.Value = ""
	ti.CursorPos = 0
}