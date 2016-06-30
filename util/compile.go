package util

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"path"
	"strings"

	"github.com/ebuchman/go-shell-pipes"
	log "github.com/eris-ltd/eris-logger"
)

type Compiler struct {
	config LangConfig
	lang   string
}

// Compile request object
type Request struct {
	ScriptName string                    `json:"name"`
	Language   string                    `json:"language"`
	Includes   map[string]*IncludedFiles `json:"includes"`  // our required files and metadata
	Libraries  string                    `json:"libraries"` // string of libName:LibAddr separated by comma
	Optimize   bool                      `json:"optimize"`  // run with optimize flag
}

// this handles all of our imports
type IncludedFiles struct {
	ObjectNames []string `json:"objectNames"` //objects in the file
	Script      []byte   `json:"script"`      //actual code
}

// Compile response object
type ResponseItem struct {
	Objectname string `json:"objectname"`
	Bytecode   string `json:"bytecode"`
	ABI        string `json:"abi"` // json encoded
}

type Response struct {
	Objects []ResponseItem `json:"objects"`
	Error   string         `json:"error"`
}

func RunCommand(tokens ...string) (string, error) {
	s, err := pipes.RunStrings(tokens...)
	s = strings.TrimSpace(s)
	return s, err
}

func CreateRequest(file string, libraries string, optimize bool) (*Request, error) {
	var includes = make(map[string]*IncludedFiles)

	language, err := LangFromFile(file)
	if err != nil {
		return &Request{}, err
	}
	compiler := &Compiler{
		config: Languages[language],
		lang:   language,
	}
	code, err := ioutil.ReadFile(file)
	if err != nil {
		return &Request{}, err
	}
	dir := path.Dir(file)
	//log.Debug("Before parsing includes =>\n\n%s", string(code))
	code, err = compiler.replaceIncludes(code, dir, includes)
	if err != nil {
		return &Request{}, err
	}

	return compiler.CompilerRequest(file, includes, libraries, optimize), nil

}

// New Request object from script and map of include files
func (c *Compiler) CompilerRequest(file string, includes map[string]*IncludedFiles, libs string, optimize bool) *Request {
	if includes == nil {
		includes = make(map[string]*IncludedFiles)
	}
	return &Request{
		Language:   c.lang,
		Includes:   includes,
		Libraries:  libs,
		Optimize:   optimize,
	}
}

// Compile takes a dir and some code, replaces all includes, checks cache, compiles, caches
func Compile(req *Request) *Response {

	if _, ok := Languages[req.Language]; !ok {
		return compilerResponse("", "", "", fmt.Errorf("No script provided"))
	}

	lang := Languages[req.Language]

	includes := []string{}
	currentDir, _ := os.Getwd()
	defer os.Chdir(currentDir)

	for k, v := range req.Includes {
		os.Chdir(lang.CacheDir)
		file, err := createTemporaryFile(k, v.Script)
		if err != nil {
			return compilerResponse("", "", "", err)
		}
		defer os.Remove(file.Name())
		includes = append(includes, file.Name())
		log.WithField("Filepath of include: ", file.Name()).Debug("To Cache")
	}

	command := lang.Cmd(includes, req.Libraries, req.Optimize)
	log.WithField("Command: ", command).Debug("Command Input")
	hexCode, err := RunCommand(command...)
	//cleanup

	if err != nil {
		log.WithFields(log.Fields{
			"err":     err,
			"command": command,
		}).Error("Could not compile", err)
		return compilerResponse("", "", "", err)
	}

	solcResp := BlankSolcResponse()

	//todo: provide unmarshalling for serpent and lll
	log.WithField("Json: ", hexCode).Debug("Command Output")
	err = json.Unmarshal([]byte(hexCode), solcResp)
	if err != nil {
		log.Debug("Could not unmarshal json")
		return compilerResponse("", "", "", err)
	}
	respItemArray := make([]ResponseItem, 0)

	for contract, item := range solcResp.Contracts {
		respItem := ResponseItem{
			Objectname: strings.TrimSpace(contract),
			Bytecode:   strings.TrimSpace(item.Bin),
			ABI:        strings.TrimSpace(item.Abi),
		}
		respItemArray = append(respItemArray, respItem)
	}

	for _, re := range respItemArray {
		log.WithFields(log.Fields{
			"name": re.Objectname,
			"bin":  re.Bytecode,
			"abi":  re.ABI,
		}).Warn("Response formulated")
	}

	log.Debug("\n")
	return &Response{
		Objects: respItemArray,
		Error:   "",
	}
}

// Fill in the filename and return the command line args
func (l LangConfig) Cmd(includes []string, libraries string, optimize bool) (args []string) {
	for _, s := range l.CompileCmd {
		if s == "_" {
			args = append(args, includes...)
		} else {
			args = append(args, s)
		}
	}
	if optimize {
		args = append(args, "--optimize")
	}
	if libraries != "" {
		args = append(args, "--libraries ")
		args = append(args, libraries)
	}
	return
}

// New response object from bytecode and an error
func compilerResponse(objectname string, bytecode string, abi string, err error) *Response {
	e := ""
	if err != nil {
		e = err.Error()
	}

	respItem := ResponseItem{
		Objectname: objectname,
		Bytecode:   bytecode,
		ABI:        abi}

	respItemArray := make([]ResponseItem, 1)
	respItemArray[0] = respItem

	return &Response{
		Objects: respItemArray,
		Error:   e,
	}
}
