package util

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path"
	"strings"

	log "github.com/eris-ltd/eris-logger"
)

type Compiler struct {
	config LangConfig
	lang   string
}

// Compile request object
type Request struct {
	ScriptName      string                    `json:"name"`
	Language        string                    `json:"language"`
	Includes        map[string]*IncludedFiles `json:"includes"`  // our required files and metadata
	Libraries       string                    `json:"libraries"` // string of libName:LibAddr separated by comma
	Optimize        bool                      `json:"optimize"`  // run with optimize flag
	FileReplacement map[string]string         `json:"replacement"`
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
	Version string  	   `json:"version"`
	Error   string         `json:"error"`
}

func RunCommand(tokens ...string) (string, error) {
	cmd := tokens[0]
	args := tokens[1:]
	shellCmd := exec.Command(cmd, args...)
	output, err := shellCmd.CombinedOutput()
	s := strings.TrimSpace(string(output))
	return s, err
}

func CreateRequest(file string, libraries string, optimize bool) (*Request, error) {
	var includes = make(map[string]*IncludedFiles)

	//maps hashes to original file name
	var hashFileReplacement = make(map[string]string)
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
	code, err = compiler.replaceIncludes(code, dir, file, includes, hashFileReplacement)
	if err != nil {
		return &Request{}, err
	}

	return compiler.CompilerRequest(file, includes, libraries, optimize, hashFileReplacement), nil

}

// New Request object from script and map of include files
func (c *Compiler) CompilerRequest(file string, includes map[string]*IncludedFiles, libs string, optimize bool, hashFileReplacement map[string]string) *Request {
	if includes == nil {
		includes = make(map[string]*IncludedFiles)
	}
	return &Request{
		Language:        c.lang,
		Includes:        includes,
		Libraries:       libs,
		Optimize:        optimize,
		FileReplacement: hashFileReplacement,
	}
}

// Compile takes a dir and some code, replaces all includes, checks cache, compiles, caches
func Compile(req *Request) *Response {

	if _, ok := Languages[req.Language]; !ok {
		return compilerResponse("", "", "", "", fmt.Errorf("No script provided"))
	}

	lang := Languages[req.Language]

	includes := []string{}
	currentDir, _ := os.Getwd()
	defer os.Chdir(currentDir)

	for k, v := range req.Includes {
		os.Chdir(lang.CacheDir)
		file, err := createTemporaryFile(k, v.Script)
		if err != nil {
			return compilerResponse("", "", "", "", err)
		}
		defer os.Remove(file.Name())
		includes = append(includes, file.Name())
		log.WithField("Filepath of include: ", file.Name()).Debug("To Cache")
	}

	command := lang.Cmd(includes, req.Libraries, req.Optimize)
	log.WithField("Command: ", command).Debug("Command Input")
	hexCode, err := RunCommand(command...)
	//cleanup
	log.WithField("=>", hexCode).Debug("Output from command: ")
	if err != nil {
		for _, str := range includes {
			hexCode = strings.Replace(hexCode, str, req.FileReplacement[str], -1)
		}
		log.WithFields(log.Fields{
			"err":      err,
			"command":  command,
			"response": hexCode,
		}).Debug("Could not compile")
		return compilerResponse("", "", "", "", fmt.Errorf("%v", hexCode))
	}

	solcResp := BlankSolcResponse()

	//todo: provide unmarshalling for serpent and lll
	log.WithField("Json: ", hexCode).Debug("Command Output")
	err = json.Unmarshal([]byte(hexCode), solcResp)
	if err != nil {
		log.Debug("Could not unmarshal json")
		return compilerResponse("", "", "", "", err)
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
			"_name": re.Objectname,
			"bin":   re.Bytecode,
			"abi":   re.ABI,
		}).Debug("Response formulated")
	}

	return &Response{
		Objects: respItemArray,
		Version: solcResp.Version,
		Error:   "",
	}
}

// Fill in the filename and return the command line args
func (l LangConfig) Cmd(includes []string, libraries string, optimize bool) (args []string) {
	for _, s := range l.CompileCmd {
		if s == "_" {
			if optimize {
				args = append(args, "--optimize")
			}
			if libraries != "" {
				args = append(args, "--libraries")
				args = append(args, libraries)
			}
			args = append(args, includes...)
		} else {
			args = append(args, s)
		}
	}
	return
}

// New response object from bytecode and an error
func compilerResponse(objectname string, bytecode string, abi string, version string, err error) *Response {
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
		Version: version,
		Error:   e,
	}
}
