package graphql

import (
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"strings"
	//"log"

	. "github.com/playlyfe/go-graphql/language"
	"github.com/playlyfe/go-graphql/utils"
)

type ResolveParams struct {
	Executor *Executor
	Request  *Document
	Schema   *Document
	Context  interface{}
	Source   interface{}
	Args     map[string]interface{}
	Field    *Field
}

type Error struct {
	Error error
	Field *Field
}

type RequestContext struct {
	AppContext              interface{}
	Document                *Document
	Errors                  []*Error
	Variables               map[string]interface{}
	VariableDefinitionIndex map[string]*VariableDefinition
}

type FieldParams struct {
	Resolve func(params *ResolveParams) (interface{}, error)
	Before  func(params *ResolveParams) error
	After   func(params *ResolveParams) error
}

type Scalar struct {
	ParseLiteral func(value interface{}) (interface{}, error)
	ParseValue   func(value interface{}) (interface{}, error)
	Serialize    func(value interface{}) (interface{}, error)
}

type Executor struct {
	ResolveType  func(value interface{}) string
	IsNullish    func(value interface{}) bool
	Schema       *Schema
	Resolvers    map[string]interface{}
	Scalars      map[string]*Scalar
	ErrorHandler func(err *Error) map[string]interface{}
}

/**
 * Prepares an object map of variableValues of the correct type based on the
 * provided variable definitions and arbitrary input. If the input cannot be
 * parsed to match the variable definitions, a GraphQLError will be thrown.
 */
/*func (executor *Executor) variableValues(definitionASTs []*VariableDefinition, inputs map[string]interface{}) (map[string]interface{}, error) {
	values := map[string]interface{}{}
	for _, definitionAST := range definitionASTs {
		varName := definitionAST.Variable.Name.Value
		varValue, err := executor.variableValue(definitionAST.Type, inputs[varName])
		if err != nil {
			return nil, err
		}
		values[varName] = varValue
	}
	return values, nil
}*/

func (executor *Executor) resolveNamedType(ntype ASTNode) *NamedType {
	unmodifiedType := ntype
	for {
		if untype, ok := unmodifiedType.(*NonNullType); ok {
			unmodifiedType = untype.Type
		} else if untype, ok := unmodifiedType.(*ListType); ok {
			unmodifiedType = untype.Type
		} else {
			return unmodifiedType.(*NamedType)
		}
	}
	return nil
}

// TODO: Implement type printer
/*func PrintType (ntype ASTNode) string {
    output := ""


    }
}*/

/**
 * Prepares an object map of argument values given a list of argument
 * definitions and list of argument AST nodes.
 */
func (executor *Executor) argumentValues(argDefs map[string]*InputValueDefinition, argASTMap map[string]*Argument, variableValues map[string]interface{}, variableDefinitionIndex map[string]*VariableDefinition) (map[string]interface{}, error) {
	result := map[string]interface{}{}
	for _, argDef := range argDefs {
		var valueAST ASTNode
		name := argDef.Name.Value
		if argAST, ok := argASTMap[name]; ok {
			valueAST = argAST.Value
		}
		value, err := executor.valueFromAST(valueAST, argDef.Type, variableValues, variableDefinitionIndex)
		if err != nil {
			return nil, err
		}
		if executor.IsNullish(value) {
			value, err = executor.valueFromAST(argDef.DefaultValue, argDef.Type, nil, nil)
			if err != nil {
				return nil, err
			}
		}
		if !executor.IsNullish(value) {
			result[name] = value
		}
	}
	return result, nil
}

func (executor *Executor) variableValue(ntype ASTNode, input interface{}) (interface{}, error) {
	if ttype, ok := ntype.(*NonNullType); ok {
		value, err := executor.variableValue(ttype.Type, input)
		if err != nil {
			return nil, err
		}
		if executor.IsNullish(value) {
			if namedType, ok := ttype.Type.(*NamedType); ok {
				return nil, &GraphQLError{
					Message: fmt.Sprintf("Expected \"%s!\", found null", namedType.Name.Value),
				}
			}
			return nil, &GraphQLError{
				Message: "Expected non-null value, found null",
			}
		}
		return value, nil
	}
	if input == nil {
		return nil, nil
	}
	if ttype, ok := ntype.(*ListType); ok {
		result := []interface{}{}
		if list, ok := input.([]interface{}); ok {
			for index, item := range list {
				value, err := executor.variableValue(ttype.Type, item)
				if err != nil {
					if gqlErr, ok := err.(*GraphQLError); ok {
						return nil, &GraphQLError{
							Message: fmt.Sprintf("In element #%d: %s", index, gqlErr.Message),
						}
					}
					return nil, err
				}
				result = append(result, value)
			}
		} else {
			value, err := executor.variableValue(ttype.Type, input)
			if err != nil {
				return nil, err
			}
			result = append(result, value)
		}
		return result, nil
	}
	if ttype, ok := ntype.(*NamedType); ok {
		switch typeName := ttype.Name.Value; typeName {
		case "Int":
			result, ok := utils.CoerceInt(input)
			if !ok {
				return nil, &GraphQLError{
					Message: "Failed to coerce value to Int",
				}
			}
			return result, nil
		case "Float":
			result, ok := utils.CoerceFloat(input)
			if !ok {
				return nil, &GraphQLError{
					Message: "Failed to coerce value to Float",
				}
			}
			return result, nil
		case "String", "ID":
			result, ok := utils.CoerceString(input)
			if !ok {
				return nil, &GraphQLError{
					Message: "Failed to coerce value to String",
				}
			}
			return result, nil
		case "Boolean":
			result, ok := utils.CoerceBoolean(input)
			if !ok {
				return nil, &GraphQLError{
					Message: "Failed to coerce value to Boolean",
				}
			}
			return result, nil
		default:
			ttype := executor.Schema.Document.TypeIndex[typeName]
			if inputType, ok := ttype.(*InputObjectTypeDefinition); ok {
				fields := inputType.Fields
				if object, ok := input.(map[string]interface{}); ok {
					result := map[string]interface{}{}
					for _, field := range fields {
						if fieldValue, ok := object[field.Name.Value]; ok {
							fieldValue, err := executor.variableValue(field.Type, fieldValue)
							if err != nil {
								if gqlErr, ok := err.(*GraphQLError); ok {
									return nil, &GraphQLError{
										Message: fmt.Sprintf("In field %q: %s", field.Name.Value, gqlErr.Message),
									}
								}
								return nil, err
							}
							if executor.IsNullish(fieldValue) {
								fieldValue, err = executor.valueFromAST(field.DefaultValue, field.Type, nil, nil)
								if err != nil {
									return nil, err
								}
							}
							if !executor.IsNullish(fieldValue) {
								result[field.Name.Value] = fieldValue
							}
							delete(object, field.Name.Value)
						} else {
							// We ensure that the missing value is nullable
							_, err := executor.variableValue(field.Type, nil)
							if err != nil {
								if gqlErr, ok := err.(*GraphQLError); ok {
									return nil, &GraphQLError{
										Message: fmt.Sprintf("In field %q: %s", field.Name.Value, gqlErr.Message),
									}
								}
								return nil, err
							}
						}
					}
					for key, _ := range object {
						return nil, &GraphQLError{
							Message: fmt.Sprintf("In field %q: Unknown field", key),
						}
					}
					return result, nil
				} else {
					return nil, &GraphQLError{
						Message: fmt.Sprintf("Expected %q, found not an object", typeName),
					}
				}
			}
			if _, ok := ttype.(*EnumTypeDefinition); ok {
				result, ok := utils.CoerceString(input)
				if !ok {
					return nil, &GraphQLError{
						Message: "Failed to coerce enum value to String",
					}
				}
				return result, nil
			}

			if scalar, ok := ttype.(*ScalarTypeDefinition); ok {
				// TODO: Handle coercing of value for custom scalars!
				if parser, ok := executor.Scalars[scalar.Name.Value]; ok {
					result, err := parser.ParseValue(input)
					if err != nil {
						return nil, err
					}
					return result, nil
				}
				return nil, &GraphQLError{
					Message: fmt.Sprintf("Scalar %s has not been implemented", scalar.Name.Value),
				}
			}

			return nil, &GraphQLError{
				Message: "Must be input type",
			}
		}
	}
	return nil, &GraphQLError{
		Message: "Unknown variable input type" + reflect.TypeOf(ntype).Elem().Name(),
	}
}

func (executor *Executor) valueFromAST(valueAST ASTNode, ntype ASTNode, variables map[string]interface{}, variableDefinitionIndex map[string]*VariableDefinition) (interface{}, error) {
	if ttype, ok := ntype.(*NonNullType); ok {
		// Note: we're not checking that the result of valueFromAST is non-null.
		// We're assuming that this query has been validated and the value used
		// here is of the correct type.
		return executor.valueFromAST(valueAST, ttype.Type, variables, variableDefinitionIndex)
	}
	if valueAST == nil {
		return nil, nil
	}
	if ttype, ok := valueAST.(*Variable); ok {
		variableName := ttype.Name.Value
		if variables == nil {
			return nil, &GraphQLError{
				Message: "No variables passed",
			}
		}
		value, ok := variables[variableName]
		field := variableDefinitionIndex[variableName]
		if field != nil {
			namedType := executor.resolveNamedType(field.Type)
			variableType := executor.Schema.Document.TypeIndex[namedType.Name.Value]
			_, isInputType := variableType.(*InputObjectTypeDefinition)
			_, isScalar := variableType.(*ScalarTypeDefinition)
			_, isEnum := variableType.(*EnumTypeDefinition)
			if !isInputType && !isScalar && !isEnum {
				return nil, &GraphQLError{
					Message: fmt.Sprintf("Variable \"$%s\" expected value of type \"%s\" which cannot be used as an input type", variableName, namedType.Name.Value),
				}
			}
		}

		if !ok {
			if field != nil {
				if field.DefaultValue != nil {
					variableValue, err := executor.valueFromAST(field.DefaultValue, field.Type, nil, nil)
					if err != nil {
						return nil, err
					}
					return variableValue, nil
				}
				if nntype, ok := field.Type.(*NonNullType); ok {
					namedType := executor.resolveNamedType(nntype.Type)
					return nil, &GraphQLError{
						Message: fmt.Sprintf("Variable \"$%s\" of required type \"%s!\" was not provided", variableName, namedType.Name.Value),
					}
				}
			}
			return nil, nil
		}
		if nntype, ok := field.Type.(*NonNullType); ok && value == nil {
			namedType := executor.resolveNamedType(nntype.Type)
			return nil, &GraphQLError{
				Message: fmt.Sprintf("Variable \"$%s\" of required type \"%s!\" was not provided", variableName, namedType.Name.Value),
			}
		}

		result, err := executor.variableValue(variableDefinitionIndex[variableName].Type, value)
		if err != nil {
			if gqlErr, ok := err.(*GraphQLError); ok {
				return nil, &GraphQLError{
					Message: fmt.Sprintf("Variable \"$%s\" got invalid value \n%s", variableName, gqlErr.Message),
				}
			}
		}
		return result, nil
	}
	if ttype, ok := ntype.(*ListType); ok {
		itemType := ttype.Type
		if list, ok := valueAST.(*List); ok {
			result := []interface{}{}
			for _, itemAST := range list.Values {
				itemValue, err := executor.valueFromAST(itemAST, itemType, variables, variableDefinitionIndex)
				if err != nil {
					return nil, err
				}
				result = append(result, itemValue)
			}
			return result, nil
		} else {
			itemValue, err := executor.valueFromAST(valueAST, itemType, variables, variableDefinitionIndex)
			if err != nil {
				return nil, err
			}
			return []interface{}{itemValue}, nil
			/*return nil, &GraphQLError{
							Message: "Epected a list",
			            }*/
		}
	}
	if vtype, ok := ntype.(*NamedType); ok {
		switch typeName := vtype.Name.Value; typeName {
		case "Int":
			if val, ok := valueAST.(*Int); ok {
				return val.Value, nil
			}
		case "Float":
			if val, ok := valueAST.(*Float); ok {
				return val.Value, nil
			}
		case "String", "ID":
			if val, ok := valueAST.(*String); ok {
				return val.Value, nil
			}
		case "Boolean":
			if val, ok := valueAST.(*Boolean); ok {
				return val.Value, nil
			}
		default:
			ttype := executor.Schema.Document.TypeIndex[typeName]
			if ttype == nil {
				return nil, &GraphQLError{
					Message: fmt.Sprintf("Type %s not defined", typeName),
				}
			}
			if inputType, ok := ttype.(*InputObjectTypeDefinition); ok {
				fields := inputType.Fields
				if object, ok := valueAST.(*Object); ok {
					fieldASTs := object.FieldIndex
					result := map[string]interface{}{}
					for _, field := range fields {
						var fieldAST *ObjectField
						var fieldValue interface{}
						var err error
						if v, ok := fieldASTs[field.Name.Value]; ok {
							fieldAST = v
						}
						if fieldAST != nil {
							fieldValue, err = executor.valueFromAST(fieldAST.Value, field.Type, variables, variableDefinitionIndex)
							if err != nil {
								return nil, err
							}
						}
						if executor.IsNullish(fieldValue) {
							fieldValue, err = executor.valueFromAST(field.DefaultValue, field.Type, nil, nil)
							if err != nil {
								return nil, err
							}
						}
						if !executor.IsNullish(fieldValue) {
							result[field.Name.Value] = fieldValue
						}
					}
					return result, nil
				}
			}
			if _, ok := ttype.(*EnumTypeDefinition); ok {
				if val, ok := valueAST.(*Enum); ok {
					return val.Value, nil
				}
			}
			if scalar, ok := ttype.(*ScalarTypeDefinition); ok {
				// TODO: Handle parsing of AST for custom scalars!
				if scalarParser, ok := executor.Scalars[scalar.Name.Value]; ok {
					result, err := scalarParser.ParseLiteral(valueAST)
					if err != nil {
						return nil, err
					}
					return result, nil
				}
				return nil, &GraphQLError{
					Message: fmt.Sprintf("Scalar %s has not been implemented", scalar.Name.Value),
				}
			}
		}

	}
	/*return nil, &GraphQLError{
		Message: fmt.Sprintf("Unknown AST value type: %#v", ntype.(*NamedType).Name.Value),
	}*/
	return nil, nil
}

func NewExecutor(schemaDefinition string, queryRoot string, mutationRoot string, resolvers map[string]interface{}) (*Executor, error) {
	schema, schemaResolvers, err := NewSchema(schemaDefinition, queryRoot, mutationRoot)
	if err != nil {
		return nil, err
	}
	for key, schemaResolver := range schemaResolvers {
		resolvers[key] = schemaResolver
	}

	return &Executor{
		Schema:    schema,
		Resolvers: resolvers,
		Scalars:   map[string]*Scalar{},
		IsNullish: func(value interface{}) bool {
			if value, ok := value.(string); ok {
				return value == ""
			}
			if value, ok := value.(int); ok {
				return math.IsNaN(float64(value))
			}
			if value, ok := value.(float32); ok {
				return math.IsNaN(float64(value))
			}
			if value, ok := value.(float64); ok {
				return math.IsNaN(value)
			}
			return value == nil
		},
		ErrorHandler: func(err *Error) map[string]interface{} {
			result := map[string]interface{}{
				"message": err.Error.Error(),
			}
			if err.Field != nil {
				result["locations"] = []map[string]interface{}{
					{
						"line":   err.Field.Name.LOC.Start.Line,
						"column": err.Field.Name.LOC.Start.Column,
					},
				}
			}
			return result
		},
	}, nil
}

func (executor *Executor) PrintSchema() string {
	introspectionQuery := `
    query IntrospectionQuery {
        __schema {
            queryType { name }
            mutationType { name }
            subscriptionType { name }
            types {
                ...FullType
            }
            directives {
                name
                description
                args {
                    ...InputValue
                }
                onOperation
                onFragment
                onField
            }
        }
    }
    fragment FullType on __Type {
        kind
        name
        description
        fields(includeDeprecated: true) {
            name
            description
            args {
                ...InputValue
            }
            type {
                ...TypeRef
            }
            isDeprecated
            deprecationReason
        }
        inputFields {
            ...InputValue
        }
        interfaces {
            ...TypeRef
        }
        enumValues(includeDeprecated: true) {
            name
            description
            isDeprecated
            deprecationReason
        }
        possibleTypes {
            ...TypeRef
        }
    }
    fragment InputValue on __InputValue {
        name
        description
        type { ...TypeRef }
        defaultValue
    }
    fragment TypeRef on __Type {
        kind
        name
        ofType {
            kind
            name
            ofType {
                kind
                name
                ofType {
                    kind
                    name
                }
            }
        }
    }
    `
	result, err := executor.Execute(nil, introspectionQuery, map[string]interface{}{}, "IntrospectionQuery")
	if err != nil {
		panic(err)
	}
	output, err := json.MarshalIndent(result, "", "    ")
	if err != nil {
		panic(err)
	}
	return string(output)
}

func (executor *Executor) Execute(context interface{}, request string, variables map[string]interface{}, operationName string) (map[string]interface{}, error) {
	parser := &Parser{}

	document, err := parser.Parse(&ParseParams{
		Source: request,
	})
	if err != nil {
		return nil, err
	}

	result := map[string]interface{}{}
	reqCtx := &RequestContext{
		AppContext: context,
		Document:   document,
		Errors:     []*Error{},
		Variables:  variables,
	}

	notFound := true

	for _, definition := range document.Definitions {
		switch operationDefinition := definition.(type) {
		case *OperationDefinition:
			if (operationDefinition.Name != nil && operationDefinition.Name.Value == operationName) || operationName == "" {
				if operationName == "" && notFound == false {
					reqCtx.Errors = append(reqCtx.Errors, &Error{
						Error: &GraphQLError{
							Message: "GraphQL Runtime Error: Must provide operation name if query contains multiple operations",
						},
					})
				}
				notFound = false
				if operationDefinition.Operation == "query" {
					reqCtx.VariableDefinitionIndex = operationDefinition.VariableDefinitionIndex
					data, err := executor.selectionSet(reqCtx, executor.Schema.QueryRoot, map[string]interface{}{}, operationDefinition.SelectionSet)
					if err != nil {
						return nil, err
					}
					result["data"] = data
				} else if operationDefinition.Operation == "mutation" {
					reqCtx.VariableDefinitionIndex = operationDefinition.VariableDefinitionIndex
					data, err := executor.selectionSet(reqCtx, executor.Schema.MutationRoot, map[string]interface{}{}, operationDefinition.SelectionSet)
					if err != nil {
						return nil, err
					}
					result["data"] = data
				}
			}
		}
	}
	if notFound {
		reqCtx.Errors = append(reqCtx.Errors, &Error{
			Error: &GraphQLError{
				Message: fmt.Sprintf("GraphQL Runtime Error: Operation with name %q not found in document", operationName),
			},
		})
	}

	if len(reqCtx.Errors) > 0 {
		errs := []map[string]interface{}{}
		for _, err := range reqCtx.Errors {
			errs = append(errs, executor.ErrorHandler(err))
		}
		result["errors"] = errs
	}
	return result, nil
}

func (executor *Executor) selectionSet(reqCtx *RequestContext, objectType *ObjectTypeDefinition, source interface{}, selectionSet *SelectionSet) (map[string]interface{}, error) {
	//log.Printf("collecting fields")
	groupedFields, err := executor.collectFields(reqCtx, objectType, selectionSet, &utils.Set{})
	if err != nil {
		return nil, err
	}
	//log.Printf("resolving fields")
	return executor.resolveGroupedFields(reqCtx, objectType, source, groupedFields)

}

func (executor *Executor) collectFields(reqCtx *RequestContext, objectType *ObjectTypeDefinition, selectionSet *SelectionSet, visitedFragments *utils.Set) (map[string][]*Field, error) {
	groupedFields := map[string][]*Field{}
	for _, item := range selectionSet.Selections {
		// if skipDirective's if argument is true then continue
		// if includeDirective's if argument is false then continue
		switch selection := item.(type) {
		case *Field:
			if selection.DirectiveIndex != nil && selection.DirectiveIndex["skip"] != nil {
				if selection.DirectiveIndex["skip"].ArgumentIndex != nil && selection.DirectiveIndex["skip"].ArgumentIndex["if"] != nil {
					value, err := executor.valueFromAST(selection.DirectiveIndex["skip"].ArgumentIndex["if"].Value, &NamedType{Name: &Name{Value: "Boolean"}}, reqCtx.Variables, reqCtx.VariableDefinitionIndex)
					if err != nil {
						return nil, err
					}
					if value.(bool) == true {
						continue
					}
				}
			}
			if selection.DirectiveIndex != nil && selection.DirectiveIndex["include"] != nil {
				if selection.DirectiveIndex["include"].ArgumentIndex != nil && selection.DirectiveIndex["include"].ArgumentIndex["if"] != nil {
					value, err := executor.valueFromAST(selection.DirectiveIndex["include"].ArgumentIndex["if"].Value, &NamedType{Name: &Name{Value: "Boolean"}}, reqCtx.Variables, reqCtx.VariableDefinitionIndex)
					if err != nil {
						return nil, err
					}
					if value.(bool) == false {
						continue
					}
				}
			}

			var responseKey string
			var groupForResponseKey []*Field
			var ok bool

			if selection.Alias != nil {
				responseKey = selection.Alias.Value
			} else {
				responseKey = selection.Name.Value
			}
			if groupForResponseKey, ok = groupedFields[responseKey]; !ok {
				groupedFields[responseKey] = []*Field{}
				groupForResponseKey = groupedFields[responseKey]
			}
			groupedFields[responseKey] = append(groupForResponseKey, selection)
			//log.Printf("adding selection '%s' to grouped fields of '%s'", selection.Name.Value, responseKey)
		case *FragmentSpread:
			var fragmentSpreadName string
			fragmentSpreadName = selection.Name.Value
			if visitedFragments.Has(fragmentSpreadName) {
				continue
			}
			visitedFragments.Add(fragmentSpreadName, true)
			//log.Printf("evaluating fragment spread '%s'", fragmentSpreadName)
			if reqCtx.Document.FragmentIndex == nil {
				continue
			}
			fragment, ok := reqCtx.Document.FragmentIndex[fragmentSpreadName]
			if !ok {
				continue
			}
			if selection.DirectiveIndex != nil && selection.DirectiveIndex["skip"] != nil {
				if selection.DirectiveIndex["skip"].ArgumentIndex != nil && selection.DirectiveIndex["skip"].ArgumentIndex["if"] != nil {
					value, err := executor.valueFromAST(selection.DirectiveIndex["skip"].ArgumentIndex["if"].Value, &NamedType{Name: &Name{Value: "Boolean"}}, reqCtx.Variables, reqCtx.VariableDefinitionIndex)
					if err != nil {
						return nil, err
					}
					if value.(bool) == true {
						continue
					}
				}
			}
			if selection.DirectiveIndex != nil && selection.DirectiveIndex["include"] != nil {
				if selection.DirectiveIndex["include"].ArgumentIndex != nil && selection.DirectiveIndex["include"].ArgumentIndex["if"] != nil {
					value, err := executor.valueFromAST(selection.DirectiveIndex["include"].ArgumentIndex["if"].Value, &NamedType{Name: &Name{Value: "Boolean"}}, reqCtx.Variables, reqCtx.VariableDefinitionIndex)
					if err != nil {
						return nil, err
					}
					if value.(bool) == false {
						continue
					}
				}
			}
			if fragment.DirectiveIndex != nil && fragment.DirectiveIndex["skip"] != nil {
				if fragment.DirectiveIndex["skip"].ArgumentIndex != nil && fragment.DirectiveIndex["skip"].ArgumentIndex["if"] != nil {
					value, err := executor.valueFromAST(fragment.DirectiveIndex["skip"].ArgumentIndex["if"].Value, &NamedType{Name: &Name{Value: "Boolean"}}, reqCtx.Variables, reqCtx.VariableDefinitionIndex)
					if err != nil {
						return nil, err
					}
					if value.(bool) == true {
						continue
					}
				}
			}
			if fragment.DirectiveIndex != nil && fragment.DirectiveIndex["include"] != nil {
				if fragment.DirectiveIndex["include"].ArgumentIndex != nil && fragment.DirectiveIndex["include"].ArgumentIndex["if"] != nil {
					value, err := executor.valueFromAST(fragment.DirectiveIndex["include"].ArgumentIndex["if"].Value, &NamedType{Name: &Name{Value: "Boolean"}}, reqCtx.Variables, reqCtx.VariableDefinitionIndex)
					if err != nil {
						return nil, err
					}
					if value.(bool) == false {
						continue
					}
				}
			}
			if !executor.doesFragmentTypeApply(objectType, fragment.TypeCondition) {
				continue
			}
			fragmentGroupedFields, err := executor.collectFields(reqCtx, objectType, fragment.SelectionSet, visitedFragments)
			if err != nil {
				return nil, err
			}
			for responseKey, fragmentGroup := range fragmentGroupedFields {
				var groupForResponseKey []*Field
				if groupForResponseKey, ok = groupedFields[responseKey]; !ok {
					groupedFields[responseKey] = []*Field{}
					groupForResponseKey = groupedFields[responseKey]
				}
				groupedFields[responseKey] = append(groupForResponseKey, fragmentGroup...)
			}
		case *InlineFragment:
			if selection.DirectiveIndex != nil && selection.DirectiveIndex["skip"] != nil {
				if selection.DirectiveIndex["skip"].ArgumentIndex != nil && selection.DirectiveIndex["skip"].ArgumentIndex["if"] != nil {
					value, err := executor.valueFromAST(selection.DirectiveIndex["skip"].ArgumentIndex["if"].Value, &NamedType{Name: &Name{Value: "Boolean"}}, reqCtx.Variables, reqCtx.VariableDefinitionIndex)
					if err != nil {
						return nil, err
					}
					if value.(bool) == true {
						continue
					}
				}
			}
			if selection.DirectiveIndex != nil && selection.DirectiveIndex["include"] != nil {
				if selection.DirectiveIndex["include"].ArgumentIndex != nil && selection.DirectiveIndex["include"].ArgumentIndex["if"] != nil {
					value, err := executor.valueFromAST(selection.DirectiveIndex["include"].ArgumentIndex["if"].Value, &NamedType{Name: &Name{Value: "Boolean"}}, reqCtx.Variables, reqCtx.VariableDefinitionIndex)
					if err != nil {
						return nil, err
					}
					if value.(bool) == false {
						continue
					}
				}
			}

			if selection.TypeCondition != nil && !executor.doesFragmentTypeApply(objectType, selection.TypeCondition) {
				continue
			}
			fragmentGroupedFields, err := executor.collectFields(reqCtx, objectType, selection.SelectionSet, visitedFragments)
			if err != nil {
				return nil, err
			}
			for responseKey, fragmentGroup := range fragmentGroupedFields {
				var groupForResponseKey []*Field
				var ok bool
				if groupForResponseKey, ok = groupedFields[responseKey]; !ok {
					groupedFields[responseKey] = []*Field{}
					groupForResponseKey = groupedFields[responseKey]
				}
				groupedFields[responseKey] = append(groupForResponseKey, fragmentGroup...)
			}
		default:
			panic("Unknown selection type")
		}
	}
	return groupedFields, nil
}

func (executor *Executor) doesFragmentTypeApply(objectType *ObjectTypeDefinition, fragmentType *NamedType) bool {
	typeName := fragmentType.Name.Value
	schema := executor.Schema.Document
	if objectType.Name.Value == typeName {
		return true
	}
	if possibleTypeDefinitions, ok := schema.PossibleTypesIndex[typeName]; ok {
		for _, possibleType := range possibleTypeDefinitions {
			if possibleType.Name.Value == objectType.Name.Value {
				return true
			}
		}
	}
	return false
}

func (executor *Executor) resolveGroupedFields(reqCtx *RequestContext, objectType *ObjectTypeDefinition, source interface{}, groupedFields map[string][]*Field) (map[string]interface{}, error) {
	result := map[string]interface{}{}

	// TODO: Use go routines?
	for responseKey, groupForResponseKey := range groupedFields {
		//log.Printf("evaluating field entry for '%s'", responseKey)
		key, value, err := executor.getFieldEntry(reqCtx, objectType, source, responseKey, groupForResponseKey)
		if err != nil {
			return nil, err
		}
		//log.Printf("Adding '%s' with value '%#v' to response", key, value)
		if key != "" {
			//log.Printf("Adding key '%s' with value '%#v' to response", responseKey, value)
			result[responseKey] = value
		}
	}
	return result, nil
}

func (executor *Executor) getFieldEntry(reqCtx *RequestContext, objectType *ObjectTypeDefinition, object interface{}, responseKey string, fields []*Field) (string, interface{}, error) {
	firstField := fields[0]
	//log.Printf("Test %#v", firstField.Name.Value)
	fieldType := executor.getFieldTypeFromObjectType(objectType, firstField)
	if fieldType == nil {
		//log.Printf("field type of selection '%s' could not be determined", firstField.Name.Value)
		return "", nil, nil
	}
	resolvedObject, err := executor.resolveFieldOnObject(reqCtx, objectType, object, fieldType, firstField)
	//log.Printf("field %s resolved to %#v", firstField.Name.Value, resolvedObject)
	if err != nil {
		return "", nil, err
	}
	subSelectionSet := executor.mergeSelectionSets(fields)
	responseValue, err := executor.completeValueCatchingError(reqCtx, objectType, fieldType, firstField, resolvedObject, subSelectionSet)
	if err != nil {
		return "", nil, err
	}
	if resolvedObject == nil {
		return responseKey, nil, nil
	}
	return responseKey, responseValue, nil
}

func (executor *Executor) mergeSelectionSets(fields []*Field) *SelectionSet {
	selectionSet := &SelectionSet{}
	selectionSet.Selections = []ASTNode{}
	for _, field := range fields {
		if field.SelectionSet == nil || len(field.SelectionSet.Selections) == 0 {
			continue
		}
		selectionSet.Selections = append(selectionSet.Selections, field.SelectionSet.Selections...)
	}
	return selectionSet
}

func (executor *Executor) completeValueCatchingError(reqCtx *RequestContext, objectType *ObjectTypeDefinition, fieldType ASTNode, field *Field, result interface{}, subSelectionSet *SelectionSet) (interface{}, error) {
	if _, ok := fieldType.(*NonNullType); ok {
		return executor.completeValue(reqCtx, objectType, fieldType, field, result, subSelectionSet)
	}
	result, err := executor.completeValue(reqCtx, objectType, fieldType, field, result, subSelectionSet)
	if err != nil {
		if gqlError, ok := err.(*GraphQLError); ok {
			var errField *Field
			if gqlError.Field != nil {
				errField = gqlError.Field
			}
			reqCtx.Errors = append(reqCtx.Errors, &Error{
				Error: err,
				Field: errField,
			})
			return nil, nil
		} else {
			return nil, err
		}
	}
	return result, err
}

func (executor *Executor) completeValue(reqCtx *RequestContext, objectType *ObjectTypeDefinition, fieldType ASTNode, field *Field, result interface{}, subSelectionSet *SelectionSet) (interface{}, error) {
	//var err error
	//log.Printf("completing value on %#v", result)
	if nonNullType, ok := fieldType.(*NonNullType); ok {
		innerType := nonNullType.Type
		completedResult, err := executor.completeValue(reqCtx, objectType, innerType, field, result, subSelectionSet)
		//log.Printf("completed result of %#v is %#v", result, completedResult)
		if err != nil {
			return nil, err
		}
		if completedResult == nil {
			return nil, &GraphQLError{
				Message: fmt.Sprintf("Cannot return null for non-nullable field %s.%s", objectType.Name.Value, field.Name.Value),
				Field:   field,
			}
		}
		return completedResult, nil
	}

	resultVal := reflect.ValueOf(result)
	if !resultVal.IsValid() || executor.IsNullish(result) {
		return nil, nil
	}

	if listType, ok := fieldType.(*ListType); ok {
		innerType := listType.Type
		if resultVal.Type().Kind() != reflect.Slice {
			return nil, &GraphQLError{
				Message: "Expected a list but did not find one",
				Field:   field,
			}
		}
		completedResults := []interface{}{}
		for index := 0; index < resultVal.Len(); index++ {
			val := resultVal.Index(index).Interface()
			completedItem, err := executor.completeValue(reqCtx, objectType, innerType, field, val, subSelectionSet)
			if err != nil {
				return nil, err
			}
			completedResults = append(completedResults, completedItem)
		}
		return completedResults, nil
	}
	switch typeName := fieldType.(*NamedType).Name.Value; typeName {
	case "Int":
		val, ok := utils.CoerceInt(result)
		if ok {
			return val, nil
		} else {
			return nil, nil
		}
	case "Float":
		val, ok := utils.CoerceFloat(result)
		if ok {
			return val, nil
		} else {
			return nil, nil
		}
	case "String", "ID":
		val, ok := utils.CoerceString(result)
		if ok {
			return val, nil
		} else {
			return nil, nil
		}
	case "Boolean":
		val, ok := utils.CoerceBoolean(result)
		if ok {
			return val, nil
		} else {
			return nil, nil
		}
	default:
		if _, ok := executor.Schema.Document.EnumTypeIndex[typeName]; ok {
			val, ok := utils.CoerceEnum(result)
			if ok {
				return val, nil
			} else {
				return nil, nil
			}
		}
		if scalar, ok := executor.Schema.Document.ScalarTypeIndex[typeName]; ok {
			parser, ok := executor.Scalars[typeName]
			if ok {
				val, err := parser.Serialize(result)
				if err != nil {
					return nil, err
				} else {
					return val, nil
				}
			} else {
				return nil, &GraphQLError{
					Message: fmt.Sprintf("Scalar %s has not been implemented", scalar.Name.Value),
				}
			}
		}

		if objectType, ok := executor.Schema.Document.ObjectTypeIndex[typeName]; ok {
			return executor.selectionSet(reqCtx, objectType, result, subSelectionSet)
		}
		if interfaceType, ok := executor.Schema.Document.InterfaceTypeIndex[typeName]; ok {
			objectType, err := executor.resolveAbstractType(reqCtx, field, interfaceType, result)
			if err != nil {
				return nil, err
			}
			if objectType == nil {
				return nil, nil
			}
			return executor.selectionSet(reqCtx, objectType, result, subSelectionSet)
		}
		if unionType, ok := executor.Schema.Document.UnionTypeIndex[typeName]; ok {
			objectType, err := executor.resolveAbstractType(reqCtx, field, unionType, result)
			if err != nil {
				return nil, err
			}
			if objectType == nil {
				return nil, nil
			}
			return executor.selectionSet(reqCtx, objectType, result, subSelectionSet)
		}

		return nil, &GraphQLError{
			Message: "Unknown type " + typeName,
			Field:   field,
		}
	}
	return nil, nil
}

func (executor *Executor) resolveFieldOnObject(reqCtx *RequestContext, objectType *ObjectTypeDefinition, object interface{}, fieldType ASTNode, firstField *Field) (interface{}, error) {

	resolverName := objectType.Name.Value + "/" + firstField.Name.Value
	if resolver, ok := executor.Resolvers[resolverName]; ok {
		var resolveFn func(params *ResolveParams) (interface{}, error)
		fieldParams, ok := resolver.(*FieldParams)
		if ok {
			resolveFn = fieldParams.Resolve
		} else {
			resolveFn = resolver.(func(params *ResolveParams) (interface{}, error))
		}
		args, err := executor.argumentValues(objectType.FieldIndex[firstField.Name.Value].ArgumentIndex, firstField.ArgumentIndex, reqCtx.Variables, reqCtx.VariableDefinitionIndex)
		if err != nil {
			if gqlError, ok := err.(*GraphQLError); ok {
				gqlError.Source = reqCtx.Document.LOC.Source
				gqlError.Start = firstField.Name.LOC.Start
				gqlError.End = firstField.Name.LOC.End
			}
			reqCtx.Errors = append(reqCtx.Errors, &Error{
				Error: err,
				Field: firstField,
			})
			return nil, nil
		}
		result, err := resolveFn(&ResolveParams{
			Executor: executor,
			Schema:   executor.Schema.Document,
			Request:  reqCtx.Document,
			Context:  reqCtx.AppContext,
			Source:   object,
			Args:     args,
			Field:    firstField,
		})
		if err != nil {
			// TODO: Check how to proceed
			reqCtx.Errors = append(reqCtx.Errors, &Error{
				Error: err,
				Field: firstField,
			})
			return nil, nil
			return nil, nil
		}
		return result, nil
	}

	sourceVal := reflect.ValueOf(object)
	sourceValType := sourceVal.Type()
	sourceValKind := sourceValType.Kind()
	if sourceVal.IsValid() && sourceValKind == reflect.Ptr {
		sourceVal = sourceVal.Elem()
		sourceValType = sourceVal.Type()
		sourceValKind = sourceValType.Kind()
	}
	if !sourceVal.IsValid() {
		return nil, nil
	}

	// try object as a map[string]interface
	if sourceMap, ok := object.(map[string]interface{}); ok {
		if property, ok := sourceMap[firstField.Name.Value]; ok {
			return property, nil
		}
	}

	if sourceValKind == reflect.Struct {
		// find field based on struct's json tag
		// we could potentially create a custom `graphql` tag, but its unnecessary at this point
		// since graphql speaks to client in a json-like way anyway
		// so json tags are a good way to start with
		for i := 0; i < sourceVal.NumField(); i++ {
			valueField := sourceVal.Field(i)
			typeField := sourceValType.Field(i)
			// try matching the field name first
			if typeField.Name == firstField.Name.Value {
				return valueField.Interface(), nil
			}
			tag := typeField.Tag
			jsonTag := tag.Get("json")
			jsonOptions := strings.Split(jsonTag, ",")
			if len(jsonOptions) == 0 {
				continue
			}
			if jsonOptions[0] != firstField.Name.Value {
				continue
			}
			return valueField.Interface(), nil
		}
	}

	// last resort, return nil
	return nil, nil
}

func (executor *Executor) getFieldTypeFromObjectType(objectType *ObjectTypeDefinition, firstField *Field) ASTNode {
	for _, field := range objectType.Fields {
		if field.Name.Value == firstField.Name.Value {
			return field.Type
		}
	}
	return nil
}

func (executor *Executor) resolveAbstractType(reqCtx *RequestContext, field *Field, abstractType ASTNode, value interface{}) (*ObjectTypeDefinition, error) {
	typeName := executor.ResolveType(value)
	schema := executor.Schema.Document
	if typeName == "" {
		return nil, &GraphQLError{
			Message: "The type of the value could not be determined",
			Field:   field,
		}
	}
	switch typeValue := abstractType.(type) {
	case *InterfaceTypeDefinition:
		for _, possibleType := range schema.PossibleTypesIndex[typeValue.Name.Value] {
			if possibleType.Name.Value == typeName {
				return schema.ObjectTypeIndex[typeName], nil
			}
		}
		reqCtx.Errors = append(reqCtx.Errors, &Error{
			Error: &GraphQLError{
				Message: fmt.Sprintf("GraphQL Runtime Error (%d:%d) Runtime object type %q is not a possible type for interface type %q", field.Name.LOC.Start.Line, field.Name.LOC.Start.Column, typeName, typeValue.Name.Value),
				Source:  field.Name.LOC.Source,
				Start:   field.Name.LOC.Start,
				End:     field.Name.LOC.End,
			},
			Field: field,
		})
		return nil, nil
	case *UnionTypeDefinition:
		unionType := schema.UnionTypeIndex[typeValue.Name.Value]
		for _, possibleType := range schema.PossibleTypesIndex[unionType.Name.Value] {
			if possibleType.Name.Value == typeName {
				return schema.ObjectTypeIndex[typeName], nil
			}
		}
		reqCtx.Errors = append(reqCtx.Errors, &Error{
			Error: &GraphQLError{
				Message: fmt.Sprintf("GraphQL Runtime Error (%d:%d) Runtime object type %q is not a possible type for union type %q", field.Name.LOC.Start.Line, field.Name.LOC.Start.Column, typeName, typeValue.Name.Value),
				Source:  field.LOC.Source,
				Start:   field.Name.LOC.Start,
				End:     field.Name.LOC.End,
			},
			Field: field,
		})
		return nil, nil
	default:
		panic("Invalid Abstract Type found in GraphQL Document")
	}
}