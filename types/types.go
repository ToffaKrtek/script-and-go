package types

type Script struct {
	Name  string `yaml:"name"`
	Steps []Step `yaml:"steps"`
}

type Step struct {
	SSH        string `yaml:"ssh,omitempty"`
	Cmd        string `yaml:"cmd,omitempty"`
	Input      any    `yaml:"input,omitempty"`
	Display    string `yaml:"display,omitempty"`
	InputToCmd bool   `yaml:"input_to_cmd,omitempty"`
	ShowOutput bool   `yaml:"show_output,omitempty"`
}
