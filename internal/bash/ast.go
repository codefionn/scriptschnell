// Package bash provides a bash virtual environment with AST parsing,
// command routing, and virtual filesystem support.
package bash

// NodeType represents the type of an AST node
type NodeType string

const (
	NodeCommand       NodeType = "command"        // Simple command
	NodePipeline      NodeType = "pipeline"       // Pipeline of commands
	NodeAnd           NodeType = "and"            // && operator
	NodeOr            NodeType = "or"             // || operator
	NodeList          NodeType = "list"           // ; or newline separated commands
	NodeSubshell      NodeType = "subshell"       // ( commands )
	NodeBraceGroup    NodeType = "brace_group"    // { commands }
	NodeIf            NodeType = "if"             // if/then/elif/else/fi
	NodeWhile         NodeType = "while"          // while/do/done
	NodeUntil         NodeType = "until"          // until/do/done
	NodeFor           NodeType = "for"            // for/in/do/done
	NodeCase          NodeType = "case"           // case/in/esac
	NodeFunction      NodeType = "function"       // function definition
	NodeAssignment    NodeType = "assignment"     // variable assignment
	NodeRedirect      NodeType = "redirect"       // redirection operator
	NodeHeredoc       NodeType = "heredoc"        // here-document
	NodeComment       NodeType = "comment"        // # comment
)

// Node represents a node in the bash AST
type Node interface {
	// Type returns the node type
	Type() NodeType
	
	// Position returns the position in the source
	Position() Position
	
	// String returns a string representation
	String() string
}

// Position represents a position in source code
type Position struct {
	Line   int `json:"line"`
	Column int `json:"column"`
	Offset int `json:"offset"`
}

func (p Position) LineNumber() int    { return p.Line }
func (p Position) ColumnNumber() int  { return p.Column }
func (p Position) ByteOffset() int    { return p.Offset }

// BaseNode provides common fields for all AST nodes
type BaseNode struct {
	Pos Position `json:"position"`
}

func (n BaseNode) Position() Position { return n.Pos }

// CommandNode represents a simple command with arguments
type CommandNode struct {
	BaseNode
	Name       string       `json:"name"`                 // Command name
	Arguments  []*WordNode  `json:"arguments"`            // Command arguments
	Assignments []*AssignmentNode `json:"assignments"` // Variable assignments before command
	Redirects  []*RedirectNode `json:"redirects"`       // Redirections
}

func (n *CommandNode) Type() NodeType { return NodeCommand }
func (n *CommandNode) String() string {
	result := n.Name
	for _, arg := range n.Arguments {
		result += " " + arg.String()
	}
	return result
}

// WordNode represents a word (possibly with expansions)
type WordNode struct {
	BaseNode
	Parts []WordPart `json:"parts"` // Parts of the word
	Value string     `json:"value"` // Literal value if no expansions
}

func (n *WordNode) String() string {
	if n.Value != "" && len(n.Parts) == 0 {
		return n.Value
	}
	result := ""
	for _, part := range n.Parts {
		result += part.String()
	}
	return result
}

// WordPartType represents types of word parts
type WordPartType string

const (
	WordLiteral      WordPartType = "literal"      // Literal text
	WordVariable     WordPartType = "variable"     // $VAR or ${VAR}
	WordCommandSub   WordPartType = "command_sub"  // $(command) or `command`
	WordArithmetic   WordPartType = "arithmetic"   // $((expression))
	WordQuoted       WordPartType = "quoted"       // "..." with possible expansions
	WordSingleQuoted WordPartType = "single_quoted" // '...' no expansions
)

// WordPart represents a part of a word
type WordPart interface {
	Type() WordPartType
	String() string
}

// LiteralPart represents literal text
type LiteralPart struct {
	Text string `json:"text"`
}

func (p LiteralPart) Type() WordPartType { return WordLiteral }
func (p LiteralPart) String() string     { return p.Text }

// VariablePart represents a variable expansion
type VariablePart struct {
	Name      string     `json:"name"`       // Variable name
	Modifiers []Modifier `json:"modifiers"`  // Modifiers like :-, :=, etc.
	Indirect  bool       `json:"indirect"`   // ${!name}
}

func (p VariablePart) Type() WordPartType { return WordVariable }
func (p VariablePart) String() string     { return "${" + p.Name + "}" }

// CommandSubPart represents command substitution
type CommandSubPart struct {
	Command Node   `json:"command"` // Command to execute
	Backtick bool  `json:"backtick"` // Uses backticks instead of $()
}

func (p CommandSubPart) Type() WordPartType { return WordCommandSub }
func (p CommandSubPart) String() string {
	if p.Backtick {
		return "`" + p.Command.String() + "`"
	}
	return "$(" + p.Command.String() + ")"
}

// ArithmeticPart represents arithmetic expansion
type ArithmeticPart struct {
	Expression string `json:"expression"` // Arithmetic expression
}

func (p ArithmeticPart) Type() WordPartType { return WordArithmetic }
func (p ArithmeticPart) String() string     { return "$((" + p.Expression + "))" }

// QuotedPart represents a quoted string with possible expansions
type QuotedPart struct {
	Parts []WordPart `json:"parts"` // Parts inside quotes
}

func (p QuotedPart) Type() WordPartType { return WordQuoted }
func (p QuotedPart) String() string {
	result := "\""
	for _, part := range p.Parts {
		result += part.String()
	}
	return result + "\""
}

// SingleQuotedPart represents a single-quoted string
type SingleQuotedPart struct {
	Text string `json:"text"` // Literal text
}

func (p SingleQuotedPart) Type() WordPartType { return WordSingleQuoted }
func (p SingleQuotedPart) String() string     { return "'" + p.Text + "'" }

// Modifier represents a variable modifier
type Modifier struct {
	Type     ModifierType `json:"type"`
	Parameter string      `json:"parameter"` // Modifier parameter
}

// ModifierType represents types of variable modifiers
type ModifierType string

const (
	ModDefault      ModifierType = "default"      // ${var:-default}
	ModAssignDefault ModifierType = "assign_default" // ${var:=default}
	ModError         ModifierType = "error"       // ${var:?error}
	ModAlternative   ModifierType = "alternative" // ${var:+alternative}
	ModLength        ModifierType = "length"      // ${#var}
	ModSubstring     ModifierType = "substring"   // ${var:offset:length}
	ModRemoveSmallest ModifierType = "remove_smallest" // ${var#pattern}
	ModRemoveLargest  ModifierType = "remove_largest"  // ${var##pattern}
	ModReplaceFirst   ModifierType = "replace_first"   // ${var/pattern/replacement}
	ModReplaceAll     ModifierType = "replace_all"     // ${var//pattern/replacement}
)

// PipelineNode represents a pipeline of commands
type PipelineNode struct {
	BaseNode
	Commands []Node `json:"commands"` // Commands in pipeline
	Negated  bool   `json:"negated"`  // ! before pipeline
	Time     bool   `json:"time"`     // time keyword
	TimePosix bool  `json:"time_posix"` // time -p
}

func (n *PipelineNode) Type() NodeType { return NodePipeline }
func (n *PipelineNode) String() string {
	result := ""
	if n.Negated {
		result += "! "
	}
	if n.Time {
		result += "time "
	}
	for i, cmd := range n.Commands {
		if i > 0 {
			result += " | "
		}
		result += cmd.String()
	}
	return result
}

// ListNode represents a list of commands (separated by ; or newlines)
type ListNode struct {
	BaseNode
	Commands []Node `json:"commands"` // Commands in list
}

func (n *ListNode) Type() NodeType { return NodeList }
func (n *ListNode) String() string {
	result := ""
	for i, cmd := range n.Commands {
		if i > 0 {
			result += "; "
		}
		result += cmd.String()
	}
	return result
}

// BinaryOpNode represents binary operators (&& ||)
type BinaryOpNode struct {
	BaseNode
	Operator string `json:"operator"` // "&&" or "||"
	Left     Node   `json:"left"`
	Right    Node   `json:"right"`
}

func (n *BinaryOpNode) Type() NodeType {
	if n.Operator == "&&" {
		return NodeAnd
	}
	return NodeOr
}

func (n *BinaryOpNode) String() string {
	return n.Left.String() + " " + n.Operator + " " + n.Right.String()
}

// AssignmentNode represents a variable assignment
type AssignmentNode struct {
	BaseNode
	Name  string    `json:"name"`  // Variable name
	Value *WordNode `json:"value"` // Assigned value
	Export bool     `json:"export"` // export keyword
	Local  bool     `json:"local"` // local keyword
	Declare bool    `json:"declare"` // declare/typeset keyword
	ReadOnly bool   `json:"readonly"` // readonly keyword
}

func (n *AssignmentNode) Type() NodeType { return NodeAssignment }
func (n *AssignmentNode) String() string {
	prefix := ""
	if n.Export {
		prefix = "export "
	} else if n.Local {
		prefix = "local "
	} else if n.Declare {
		prefix = "declare "
	} else if n.ReadOnly {
		prefix = "readonly "
	}
	return prefix + n.Name + "=" + n.Value.String()
}

// RedirectNode represents a redirection
type RedirectNode struct {
	BaseNode
	RedirectOp RedirectType `json:"redirect_op"`
	FileNumber int          `json:"file_number"` // File descriptor
	Target     *WordNode    `json:"target"`      // Target file or duplicator
	Close      bool         `json:"close"`       // Close file descriptor
}

// RedirectType represents types of redirections
type RedirectType string

const (
	RedirectInput      RedirectType = "input"      // <
	RedirectOutput     RedirectType = "output"     // >
	RedirectAppend     RedirectType = "append"     // >>
	RedirectInputDup   RedirectType = "input_dup"  // <&
	RedirectOutputDup  RedirectType = "output_dup" // >&
	RedirectReadWrite  RedirectType = "read_write" // <>
	RedirectClobber    RedirectType = "clobber"    // >|
)

func (n *RedirectNode) Type() NodeType { return NodeRedirect }
func (n *RedirectNode) String() string {
	fd := ""
	if n.FileNumber > 0 {
		fd = string(rune('0' + n.FileNumber))
	}
	switch n.RedirectOp {
	case RedirectInput:
		return fd + "< " + n.Target.String()
	case RedirectOutput:
		return fd + "> " + n.Target.String()
	case RedirectAppend:
		return fd + ">> " + n.Target.String()
	case RedirectInputDup:
		return fd + "<& " + n.Target.String()
	case RedirectOutputDup:
		return fd + ">& " + n.Target.String()
	case RedirectReadWrite:
		return fd + "<> " + n.Target.String()
	case RedirectClobber:
		return fd + ">| " + n.Target.String()
	}
	return fd + "? " + n.Target.String()
}

// HeredocNode represents a here-document
type HeredocNode struct {
	BaseNode
	Delimiter string `json:"delimiter"` // Heredoc delimiter
	Content   string `json:"content"`   // Content
	StripTabs bool   `json:"strip_tabs"` // <<- strips leading tabs
	Expand    bool   `json:"expand"`    // Expand variables
}

func (n *HeredocNode) Type() NodeType { return NodeHeredoc }
func (n *HeredocNode) String() string {
	op := "<<"
	if n.StripTabs {
		op = "<<-"
	}
	return op + " " + n.Delimiter + "\n" + n.Content + "\n" + n.Delimiter
}

// SubshellNode represents a subshell ( commands )
type SubshellNode struct {
	BaseNode
	Body Node `json:"body"` // Commands in subshell
}

func (n *SubshellNode) Type() NodeType { return NodeSubshell }
func (n *SubshellNode) String() string {
	return "( " + n.Body.String() + " )"
}

// BraceGroupNode represents a brace group { commands }
type BraceGroupNode struct {
	BaseNode
	Body Node `json:"body"` // Commands in brace group
}

func (n *BraceGroupNode) Type() NodeType { return NodeBraceGroup }
func (n *BraceGroupNode) String() string {
	return "{ " + n.Body.String() + "; }"
}

// IfNode represents an if statement
type IfNode struct {
	BaseNode
	Condition   Node   `json:"condition"`   // Condition to test
	ThenBody    Node   `json:"then_body"`   // Then branch
	ElifClauses []*ElifNode `json:"elif_clauses"` // Elif clauses
	ElseBody    Node   `json:"else_body"`   // Else branch
}

func (n *IfNode) Type() NodeType { return NodeIf }
func (n *IfNode) String() string {
	result := "if " + n.Condition.String() + "; then " + n.ThenBody.String()
	for _, elif := range n.ElifClauses {
		result += "; " + elif.String()
	}
	if n.ElseBody != nil {
		result += "; else " + n.ElseBody.String()
	}
	return result + "; fi"
}

// ElifNode represents an elif clause
type ElifNode struct {
	Condition Node `json:"condition"` // Condition to test
	Body      Node `json:"body"`      // Body to execute
}

func (n *ElifNode) String() string {
	return "elif " + n.Condition.String() + "; then " + n.Body.String()
}

// WhileNode represents a while loop
type WhileNode struct {
	BaseNode
	Condition Node `json:"condition"` // Condition to test
	Body      Node `json:"body"`      // Loop body
}

func (n *WhileNode) Type() NodeType { return NodeWhile }
func (n *WhileNode) String() string {
	return "while " + n.Condition.String() + "; do " + n.Body.String() + "; done"
}

// UntilNode represents an until loop
type UntilNode struct {
	BaseNode
	Condition Node `json:"condition"` // Condition to test
	Body      Node `json:"body"`      // Loop body
}

func (n *UntilNode) Type() NodeType { return NodeUntil }
func (n *UntilNode) String() string {
	return "until " + n.Condition.String() + "; do " + n.Body.String() + "; done"
}

// ForNode represents a for loop
type ForNode struct {
	BaseNode
	Variable string      `json:"variable"` // Loop variable
	Items    []*WordNode `json:"items"`    // Items to iterate over
	Body     Node        `json:"body"`     // Loop body
	ThreeExpr bool        `json:"three_expr"` // C-style for ((;;))
	Init     Node        `json:"init"`     // Init expression (C-style)
	Cond     Node        `json:"cond"`     // Condition (C-style)
	Update   Node        `json:"update"`   // Update expression (C-style)
}

func (n *ForNode) Type() NodeType { return NodeFor }
func (n *ForNode) String() string {
	if n.ThreeExpr {
		return "for ((" + n.Init.String() + "; " + n.Cond.String() + "; " + n.Update.String() + ")); do " + n.Body.String() + "; done"
	}
	result := "for " + n.Variable + " in"
	for _, item := range n.Items {
		result += " " + item.String()
	}
	return result + "; do " + n.Body.String() + "; done"
}

// CaseNode represents a case statement
type CaseNode struct {
	BaseNode
	Word   *WordNode     `json:"word"`   // Word to match
	Items  []*CaseItemNode `json:"items"` // Case items
}

func (n *CaseNode) Type() NodeType { return NodeCase }
func (n *CaseNode) String() string {
	result := "case " + n.Word.String() + " in"
	for _, item := range n.Items {
		result += " " + item.String()
	}
	return result + ";; esac"
}

// CaseItemNode represents a case item
type CaseItemNode struct {
	Patterns []*WordNode `json:"patterns"` // Patterns to match
	Body     Node        `json:"body"`     // Body to execute
}

func (n *CaseItemNode) String() string {
	result := ""
	for i, pattern := range n.Patterns {
		if i > 0 {
			result += "|"
		}
		result += pattern.String()
	}
	return result + ") " + n.Body.String() + ";;"
}

// FunctionNode represents a function definition
type FunctionNode struct {
	BaseNode
	Name string `json:"name"` // Function name
	Body Node   `json:"body"` // Function body
}

func (n *FunctionNode) Type() NodeType { return NodeFunction }
func (n *FunctionNode) String() string {
	return n.Name + "() { " + n.Body.String() + "; }"
}

// CommentNode represents a comment
type CommentNode struct {
	BaseNode
	Text string `json:"text"` // Comment text (without #)
}

func (n *CommentNode) Type() NodeType { return NodeComment }
func (n *CommentNode) String() string {
	return "#" + n.Text
}
