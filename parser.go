package main

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"regexp"
	"strings"
	"unicode"

	"google.golang.org/protobuf/compiler/protogen"
)

type Tag struct {
	Kind  string
	Value string
}

type Field struct {
	// Name represents original field name generated by protoc-gen-go command
	Name string

	// GoName represents customized field name specified with comments
	GoName string

	// Tags represent customized field tags of the field, tag 'protobuf' will be omitted
	Tags []*Tag
}

type Model struct {
	Name   string
	Fields map[string]*Field

	// Models represent nested models
	models map[string]*Model
}

type FileDescriptor struct {
	ProtoPath string
	GoPath    string
	Models    map[string]*Model
}

func (desc *FileDescriptor) parse(file *protogen.File) (err error) {
	for _, msg := range file.Messages {
		model := &Model{Name: msg.GoIdent.GoName, Fields: make(map[string]*Field), models: make(map[string]*Model)}
		if err = model.parse(msg); err != nil {
			return
		}

		desc.add(model)
	}

	return nil
}

func (desc *FileDescriptor) add(model *Model) {
	if len(model.Fields) > 0 {
		desc.Models[model.Name] = model
	}

	for _, nested := range model.models {
		desc.add(nested)
	}
}

func (model *Model) parse(msg *protogen.Message) (err error) {
	for index := range msg.Fields {
		field := &Field{Name: msg.Fields[index].GoName}
		if err = field.parse(msg.Fields[index]); err != nil {
			return
		}

		if field.GoName != "" {
			msg.Fields[index].GoName = field.GoName
			msg.Fields[index].GoIdent.GoName = model.Name + "_" + field.GoName
		}

		if len(field.Tags) > 0 {
			key := field.Name
			if field.GoName != "" {
				key = field.GoName
			}
			model.Fields[key] = field
		}
	}

	for _, nestedMessage := range msg.Messages {
		nested := &Model{Name: string(nestedMessage.GoIdent.GoName), Fields: make(map[string]*Field), models: make(map[string]*Model)}
		if err = nested.parse(nestedMessage); err != nil {
			return
		}

		if len(nested.Fields) > 0 || len(nested.models) > 0 {
			model.models[nested.Name] = nested
		}
	}

	return
}

func (field *Field) parse(pfield *protogen.Field) (err error) {
	if len(pfield.Comments.LeadingDetached) == 0 && pfield.Comments.Leading == "" && pfield.Comments.Trailing == "" {
		return
	}

	var replacement protogen.Comments
	for index, comments := range pfield.Comments.LeadingDetached {
		if replacement, err = field.parseComments(comments); err != nil {
			return
		}

		pfield.Comments.LeadingDetached[index] = replacement
	}

	if replacement, err = field.parseComments(pfield.Comments.Leading); err != nil {
		return
	}

	pfield.Comments.Leading = replacement

	if replacement, err = field.parseComments(pfield.Comments.Trailing); err != nil {
		return
	}

	pfield.Comments.Trailing = replacement

	return
}

func (field *Field) parseComments(comments protogen.Comments) (replacement protogen.Comments, err error) {
	if comments == "" {
		return
	}

	var buf bytes.Buffer

	re := regexp.MustCompile(`^@([a-z]+)\.tag=(.*)$`)
	validate := regexp.MustCompile(`[0-9a-zA-Z_]`)
	scanner := bufio.NewScanner(strings.NewReader(string(comments)))
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}

		pattern := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "*"))
		if strings.HasPrefix(pattern, "@go.name=") {
			name := pattern[9:]
			if name != "" && validate.MatchString(name) && unicode.IsUpper(rune(name[0])) {
				field.GoName = name
				continue
			}

			fmt.Fprintf(os.Stderr, "skip %s go name replacement, illegal value '%s'", field.Name, name)
		} else if matches := re.FindStringSubmatch(pattern); len(matches) == 3 {
			value := strings.TrimSpace(matches[2])
			value = strings.TrimSuffix(strings.TrimPrefix(value, "\""), "\"")
			tag := &Tag{Kind: matches[1], Value: value}
			if index := strings.IndexByte(strings.TrimSpace(tag.Value), ' '); index == -1 {
				field.Tags = append(field.Tags, tag)
				continue
			}

			fmt.Fprintf(os.Stderr, "skip commentary tag '%s' declaration, illegal value '%s'", tag.Kind, tag.Value)
		}

		buf.WriteString(line)
	}

	return protogen.Comments(buf.String()), nil
}
