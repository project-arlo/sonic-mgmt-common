////////////////////////////////////////////////////////////////////////////////
//                                                                            //
//  Copyright 2019 Dell, Inc.                                                 //
//                                                                            //
//  Licensed under the Apache License, Version 2.0 (the "License");           //
//  you may not use this file except in compliance with the License.          //
//  You may obtain a copy of the License at                                   //
//                                                                            //
//  http://www.apache.org/licenses/LICENSE-2.0                                //
//                                                                            //
//  Unless required by applicable law or agreed to in writing, software       //
//  distributed under the License is distributed on an "AS IS" BASIS,         //
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.  //
//  See the License for the specific language governing permissions and       //
//  limitations under the License.                                            //
//                                                                            //
////////////////////////////////////////////////////////////////////////////////

package transformer

import (
	"fmt"
	"encoding/json"
	"errors"
	log "github.com/golang/glog"
	"github.com/openconfig/ygot/ygot"
	"reflect"
	"strings"
	"github.com/Azure/sonic-mgmt-common/translib/db"
	"github.com/Azure/sonic-mgmt-common/translib/ocbinds"
	"github.com/Azure/sonic-mgmt-common/translib/tlerr"
)

const (
	GET = 1 + iota
	CREATE
	REPLACE
	UPDATE
	DELETE
	SUBSCRIBE
    MAXOPER
)

var XlateFuncs = make(map[string]reflect.Value)

var (
	ErrParamsNotAdapted = errors.New("The number of params is not adapted.")
)

func XlateFuncBind(name string, fn interface{}) (err error) {
	defer func() {
		if e := recover(); e != nil {
			err = errors.New(name + " is not valid Xfmr function.")
		}
	}()

	if _, ok := XlateFuncs[name]; !ok {
		v := reflect.ValueOf(fn)
		v.Type().NumIn()
		XlateFuncs[name] = v
	} else {
		xfmrLogInfo("Duplicate entry found in the XlateFunc map " + name)
	}
	return
}
func IsXlateFuncBinded(name string) bool {
    if _, ok := XlateFuncs[name]; !ok {
        return false
    } else  {
        return true
    }
}
func XlateFuncCall(name string, params ...interface{}) (result []reflect.Value, err error) {
	if _, ok := XlateFuncs[name]; !ok {
		log.Warning(name + " Xfmr function does not exist.")
		return nil, nil
	}
	if len(params) != XlateFuncs[name].Type().NumIn() {
                log.Errorf("Error parameters not adapted") 
		return nil, nil
	}
	in := make([]reflect.Value, len(params))
	for k, param := range params {
		in[k] = reflect.ValueOf(param)
	}
	result = XlateFuncs[name].Call(in)
	return result, nil
}

func TraverseDb(dbs [db.MaxDB]*db.DB, spec KeySpec, result *map[db.DBNum]map[string]map[string]db.Value, parentKey *db.Key) error {
	var dataMap = make(RedisDbMap)

	for i := db.ApplDB; i < db.MaxDB; i++ {
		dataMap[i] = make(map[string]map[string]db.Value)
	}

	err := traverseDbHelper(dbs, spec, &dataMap, parentKey)
	if err != nil {
		log.Errorf("Failed to get data from traverseDbHelper")
		return err
	}
	/* db data processing */
	curMap := make(map[int]map[db.DBNum]map[string]map[string]db.Value)
	curMap[GET] = dataMap
	err = dbDataXfmrHandler(curMap)
	if err != nil {
		log.Errorf("Failed in dbdata-xfmr")
		return err
	}

	for oper, dbData := range curMap {
		if oper == GET {
			for dbNum, tblData := range dbData {
				mapCopy((*result)[dbNum], tblData)
			}
		}
	}
	return nil
}

func traverseDbHelper(dbs [db.MaxDB]*db.DB, spec KeySpec, result *map[db.DBNum]map[string]map[string]db.Value, parentKey *db.Key) error {
	var err error
	var dbOpts db.Options = getDBOptions(spec.DbNum)

	separator := dbOpts.KeySeparator

	if spec.Key.Len() > 0 {
		// get an entry with a specific key
		if spec.Ts.Name != XFMR_NONE_STRING { // Do not traverse for NONE table
			data, err := dbs[spec.DbNum].GetEntry(&spec.Ts, spec.Key)
			if err != nil {
				log.Warningf("Failed to get data for tbl(%v), key(%v) in traverseDbHelper", spec.Ts.Name, spec.Key)
				return err
			}

			if (*result)[spec.DbNum][spec.Ts.Name] == nil {
				(*result)[spec.DbNum][spec.Ts.Name] = map[string]db.Value{strings.Join(spec.Key.Comp, separator): data}
			} else {
				(*result)[spec.DbNum][spec.Ts.Name][strings.Join(spec.Key.Comp, separator)] = data
			}
		}
		if len(spec.Child) > 0 {
			for _, ch := range spec.Child {
				err = traverseDbHelper(dbs, ch, result, &spec.Key)
			}
		}
	} else {
		// TODO - GetEntry support with regex patten, 'abc*' for optimization
		if spec.Ts.Name != XFMR_NONE_STRING { //Do not traverse for NONE table
			keys, err := dbs[spec.DbNum].GetKeys(&spec.Ts)
			if err != nil {
				log.Warningf("Failed to get keys for tbl(%v) in traverseDbHelper", spec.Ts.Name)
				return err
			}
			xfmrLogInfoAll("keys for table %v in Db %v are %v", spec.Ts.Name, spec.DbNum, keys)
			for i := range keys {
				if parentKey != nil && !spec.IgnoreParentKey {
					// TODO - multi-depth with a custom delimiter
					if !strings.Contains(strings.Join(keys[i].Comp, separator), strings.Join((*parentKey).Comp, separator)) {
						continue
					}
				}
				spec.Key = keys[i]
                                err = traverseDbHelper(dbs, spec, result, parentKey)
                                if err != nil {
                                        log.Errorf("Traversal failed for : %v", err)
                                }
			}
		} else if len(spec.Child) > 0 {
                        for _, ch := range spec.Child {
                                err = traverseDbHelper(dbs, ch, result, &spec.Key)
                        }
                }
	}
	return err
}

func XlateUriToKeySpec(uri string, requestUri string, ygRoot *ygot.GoStruct, t *interface{}, txCache interface{}) (*[]KeySpec, error) {

	var err error
	var retdbFormat = make([]KeySpec, 0)

	// In case of SONIC yang, the tablename and key info is available in the xpath
	if isSonicYang(uri) {
		/* Extract the xpath and key from input xpath */
		xpath, keyStr, tableName := sonicXpathKeyExtract(uri)
		if tblSpecInfo, ok := xDbSpecMap[tableName]; ok && tblSpecInfo.hasXfmrFn {
			/* key from uri should be converted into redis-db key, to read data */
			keyStr, err = dbKeyValueXfmrHandler(CREATE, tblSpecInfo.dbIndex, tableName, keyStr)
			if err != nil {
				log.Errorf("Value-xfmr for table(%v) & key(%v) failed.", tableName, keyStr)
				return &retdbFormat, err
			}
		}

		retdbFormat = fillSonicKeySpec(xpath, tableName, keyStr)
	} else {
		/* Extract the xpath and key from input xpath */
		xpath, keyStr, _, _ := xpathKeyExtract(nil, ygRoot, GET, uri, requestUri, nil, txCache)
		retdbFormat = FillKeySpecs(xpath, keyStr, &retdbFormat)
	}

	return &retdbFormat, err
}

func FillKeySpecs(yangXpath string , keyStr string, retdbFormat *[]KeySpec) ([]KeySpec){
	var err error
	if xYangSpecMap == nil {
		return *retdbFormat
	}
	_, ok := xYangSpecMap[yangXpath]
	if ok {
		xpathInfo := xYangSpecMap[yangXpath]
		if xpathInfo.tableName != nil {
			dbFormat := KeySpec{}
			dbFormat.Ts.Name = *xpathInfo.tableName
			dbFormat.DbNum = xpathInfo.dbIndex
			if len(xYangSpecMap[yangXpath].xfmrKey) > 0 || xYangSpecMap[yangXpath].keyName != nil {
				dbFormat.IgnoreParentKey = true
			} else {
				dbFormat.IgnoreParentKey = false
			}
			if keyStr != "" {
				if tblSpecInfo, ok := xDbSpecMap[dbFormat.Ts.Name]; ok && tblSpecInfo.hasXfmrFn {
					/* key from uri should be converted into redis-db key, to read data */
					keyStr, err = dbKeyValueXfmrHandler(CREATE, dbFormat.DbNum, dbFormat.Ts.Name, keyStr)
					if err != nil {
						log.Errorf("Value-xfmr for table(%v) & key(%v) failed.", dbFormat.Ts.Name, keyStr)
					}
				}
				dbFormat.Key.Comp = append(dbFormat.Key.Comp, keyStr)
			}
			for _, child := range xpathInfo.childTable {
				if child == dbFormat.Ts.Name {
					continue
				}
				if xDbSpecMap != nil {
					if _, ok := xDbSpecMap[child]; ok {
						chlen := len(xDbSpecMap[child].yangXpath)
						if chlen > 0 {
							children := make([]KeySpec, 0)
							for _, childXpath := range xDbSpecMap[child].yangXpath {
								children = FillKeySpecs(childXpath, "", &children)
								dbFormat.Child = append(dbFormat.Child, children...)
							}
						}
					}
				}
			}
			*retdbFormat = append(*retdbFormat, dbFormat)
		} else {
			for _, child := range xpathInfo.childTable {
				if xDbSpecMap != nil {
					if _, ok := xDbSpecMap[child]; ok {
						chlen := len(xDbSpecMap[child].yangXpath)
						if chlen > 0 {
							for _, childXpath := range xDbSpecMap[child].yangXpath {
								*retdbFormat = FillKeySpecs(childXpath, "", retdbFormat)
							}
						}
					}
				}
			}
		}
	}
	return *retdbFormat
}

func fillSonicKeySpec(xpath string , tableName string, keyStr string) ( []KeySpec ) {

	var retdbFormat = make([]KeySpec, 0)

	if tableName != "" {
		dbFormat := KeySpec{}
		dbFormat.Ts.Name = tableName
                cdb := db.ConfigDB
                if _, ok := xDbSpecMap[tableName]; ok {
			cdb = xDbSpecMap[tableName].dbIndex
                }
		dbFormat.DbNum = cdb
		if keyStr != "" {
			dbFormat.Key.Comp = append(dbFormat.Key.Comp, keyStr)
		}
		retdbFormat = append(retdbFormat, dbFormat)
	} else {
		// If table name not available in xpath get top container name
		container := xpath
		if xDbSpecMap != nil {
			if _, ok := xDbSpecMap[container]; ok {
				dbInfo := xDbSpecMap[container]
				if dbInfo.fieldType == "container" {
					for dir := range dbInfo.dbEntry.Dir {
						_, ok := xDbSpecMap[dir]
						if ok && xDbSpecMap[dir].dbEntry.Node.Statement().Keyword == "container" {
						cdb := xDbSpecMap[dir].dbIndex
						dbFormat := KeySpec{}
						dbFormat.Ts.Name = dir
						dbFormat.DbNum = cdb
						retdbFormat = append(retdbFormat, dbFormat)
						}
					}
				}
			}
		}
	}
	return retdbFormat
}

func XlateToDb(path string, opcode int, d *db.DB, yg *ygot.GoStruct, yt *interface{}, jsonPayload []byte, txCache interface{}, skipOrdTbl *bool) (map[int]RedisDbMap, map[string]map[string]db.Value, map[string]map[string]db.Value, error) {

	var err error
	requestUri := path
	jsonData := make(map[string]interface{})

	device := (*yg).(*ocbinds.Device)
	jsonStr, _ := ygot.EmitJSON(device, &ygot.EmitJSONConfig{
		Format:         ygot.RFC7951,
		Indent:         "  ",
		SkipValidation: true,
		RFC7951Config: &ygot.RFC7951JSONConfig{
			AppendModuleName: true,
		},
	})

	err = json.Unmarshal([]byte(jsonStr), &jsonData)
	if err != nil {
		errStr := "Error: failed to unmarshal json."
		log.Error(errStr)
		err = tlerr.InternalError{Format: errStr}
		return nil, nil, nil, err
	}

	// Map contains table.key.fields
	var result = make(map[int]RedisDbMap)
	var yangDefValMap = make(map[string]map[string]db.Value)
	var yangAuxValMap = make(map[string]map[string]db.Value)
	switch opcode {
	case CREATE:
		xfmrLogInfo("CREATE case")
		err = dbMapCreate(d, yg, opcode, path, requestUri, jsonData, result, yangDefValMap, yangAuxValMap, txCache)
		if err != nil {
			log.Errorf("Error: Data translation from yang to db failed for create request.")
		}

	case UPDATE:
		xfmrLogInfo("UPDATE case")
		err = dbMapUpdate(d, yg, opcode, path, requestUri, jsonData, result, yangDefValMap, yangAuxValMap, txCache)
		if err != nil {
			log.Errorf("Error: Data translation from yang to db failed for update request.")
		}

	case REPLACE:
		xfmrLogInfo("REPLACE case")
		err = dbMapUpdate(d, yg, opcode, path, requestUri, jsonData, result, yangDefValMap, yangAuxValMap, txCache)
		if err != nil {
			log.Errorf("Error: Data translation from yang to db failed for replace request.")
		}

	case DELETE:
		xfmrLogInfo("DELETE case")
		err = dbMapDelete(d, yg, opcode, path, requestUri, jsonData, result, txCache, skipOrdTbl)
		if err != nil {
			log.Errorf("Error: Data translation from yang to db failed for delete request.")
		}
	}
	return result, yangDefValMap, yangAuxValMap, err
}

func GetAndXlateFromDB(uri string, ygRoot *ygot.GoStruct, dbs [db.MaxDB]*db.DB, txCache interface{}) ([]byte, bool, error) {
	var err error
	var payload []byte
	xfmrLogInfo("received xpath = " + uri)

	requestUri := uri
	keySpec, _ := XlateUriToKeySpec(uri, requestUri, ygRoot, nil, txCache)
	var dbresult = make(RedisDbMap)
        for i := db.ApplDB; i < db.MaxDB; i++ {
                dbresult[i] = make(map[string]map[string]db.Value)
	}

	for _, spec := range *keySpec {
		err := TraverseDb(dbs, spec, &dbresult, nil)
		if err != nil {
			log.Error("TraverseDb() failure")
		}
	}

	isEmptyPayload := false
	payload, isEmptyPayload, err = XlateFromDb(uri, ygRoot, dbs, dbresult, txCache)
	if err != nil {
		log.Error("XlateFromDb() failure.")
		return payload, true, err
	}

	return payload, isEmptyPayload, err
}

func XlateFromDb(uri string, ygRoot *ygot.GoStruct, dbs [db.MaxDB]*db.DB, data RedisDbMap, txCache interface{}) ([]byte, bool, error) {

	var err error
	var result []byte
	var dbData = make(RedisDbMap)
	var cdb db.DBNum = db.ConfigDB
	var inParamsForGet xlateFromDbParams
	var xpath string

	dbData = data
	requestUri := uri
	/* Check if the parent table exists for RFC compliance */
        var exists bool
	subOpMapDiscard := make(map[int]*RedisDbMap)
        exists, err = verifyParentTable(nil, dbs, ygRoot, GET, uri, dbData, txCache, subOpMapDiscard)
        xfmrLogInfoAll("verifyParentTable() returned - exists - %v, err - %v", exists, err)
        if err != nil {
		log.Errorf("Cannot perform GET Operation on uri %v due to - %v", uri, err)
		return []byte(""), true, err
        }
        if !exists {
                err = tlerr.NotFoundError{Format:"Resource Not found"}
                return []byte(""), true, err
        }

	if isSonicYang(uri) {
		lxpath, keyStr, tableName := sonicXpathKeyExtract(uri)
		xpath = lxpath
		if (tableName != "") {
			dbInfo, ok := xDbSpecMap[tableName]
			if !ok {
				log.Warningf("No entry in xDbSpecMap for xpath %v", tableName)
			} else {
				cdb =  dbInfo.dbIndex
			}
			tokens:= strings.Split(xpath, "/")
			// Format /module:container/tableName/listname[key]/fieldName
			if tokens[SONIC_TABLE_INDEX] == tableName {
				fieldName := ""
				if len(tokens) > SONIC_FIELD_INDEX {
					fieldName = tokens[SONIC_FIELD_INDEX]
					dbSpecField := tableName + "/" + fieldName
					_, ok := xDbSpecMap[dbSpecField]
					if ok  && fieldName != "" {
						yangNodeType := yangTypeGet(xDbSpecMap[dbSpecField].dbEntry)
						if yangNodeType == YANG_LEAF_LIST {
							fieldName = fieldName + "@"
						}
						if ((yangNodeType == YANG_LEAF_LIST) || (yangNodeType == YANG_LEAF)) {
							dbData[cdb], err = extractFieldFromDb(tableName, keyStr, fieldName, data[cdb])
							// return resource not found when the leaf/leaf-list instance(not entire leaf-list GET) not found 
							if ((err != nil) && ((yangNodeType == YANG_LEAF) || ((yangNodeType == YANG_LEAF_LIST) && (strings.HasSuffix(uri, "]") || strings.HasSuffix(uri, "]/"))))) {
								return []byte(""), true, err
							}
							if ((yangNodeType == YANG_LEAF_LIST) && ((strings.HasSuffix(uri, "]")) || (strings.HasSuffix(uri, "]/")))) {
								leafListInstVal, valErr := extractLeafListInstFromUri(uri)
								if valErr != nil {
									return []byte(""), true, valErr
								}
								if leafListInstExists(dbData[cdb][tableName][keyStr].Field[fieldName], leafListInstVal) {
									/* Since translib already fills in ygRoot with queried leaf-list instance, do not
									   fill in resFldValMap or else Unmarshall of payload(resFldValMap) into ygotTgt in
									   app layer will create duplicate instances in result.
									 */
									 log.Info("Queried leaf-list instance exists.")
									 return []byte("{}"), false, nil
								} else {
									xfmrLogInfoAll("Queried leaf-list instance does not exist - %v", uri)
									return []byte(""), true, tlerr.NotFoundError{Format:"Resource not found"}
								}
							}
						}
					}
				}
			}
		}
	} else {
	        lxpath, _ := XfmrRemoveXPATHPredicates(uri)
		xpath = lxpath
		if _, ok := xYangSpecMap[xpath]; ok {
			cdb = xYangSpecMap[xpath].dbIndex
		}
	}
	inParamsForGet = formXlateFromDbParams(dbs[cdb], dbs, cdb, ygRoot, uri, requestUri, xpath, GET, "", "", &dbData, txCache, nil, false)
	payload, isEmptyPayload, err := dbDataToYangJsonCreate(inParamsForGet)
	xfmrLogInfo("Payload generated : " + payload)

	if err != nil {
		log.Errorf("Error: failed to create json response from DB data.")
		return nil, isEmptyPayload, err
	}

	result = []byte(payload)
	return result, isEmptyPayload, err

}

func extractFieldFromDb(tableName string, keyStr string, fieldName string, data map[string]map[string]db.Value) (map[string]map[string]db.Value, error) {

	var dbVal db.Value
	var dbData = make(map[string]map[string]db.Value)
	var err error

	if tableName != "" && keyStr != "" && fieldName != "" {
		if data[tableName][keyStr].Field != nil {
			fldVal, fldValExists := data[tableName][keyStr].Field[fieldName]
			if fldValExists {
				dbData[tableName] = make(map[string]db.Value)
				dbVal.Field = make(map[string]string)
				dbVal.Field[fieldName] = fldVal
				dbData[tableName][keyStr] = dbVal
			} else {
				log.Errorf("Field %v doesn't exist in table - %v, instance - %v", fieldName, tableName, keyStr)
				err = tlerr.NotFoundError{Format: "Resource not found"}
			}
		}
	}
	return dbData, err
}

func GetModuleNmFromPath(uri string) (string, error) {
	xfmrLogInfo("received uri %s to extract module name from ", uri)
	moduleNm, err := uriModuleNameGet(uri)
	return moduleNm, err
}

func GetOrdDBTblList(ygModuleNm string) ([]string, error) {
        var result []string
	var err error
        if dbTblList, ok := xDbSpecOrdTblMap[ygModuleNm]; ok {
                result = dbTblList
		if len(dbTblList) == 0 {
			log.Error("Ordered DB Table list is empty for module name = ", ygModuleNm)
			err = fmt.Errorf("Ordered DB Table list is empty for module name %v", ygModuleNm)

		}
        } else {
                log.Error("No entry found in the map of module names to ordered list of DB Tables for module = ", ygModuleNm)
                err = fmt.Errorf("No entry found in the map of module names to ordered list of DB Tables for module = %v", ygModuleNm)
        }
        return result, err
}

func GetOrdTblList(xfmrTbl string, uriModuleNm string) []string {
        var ordTblList []string
        processedTbl := false
        var sncMdlList []string = getYangMdlToSonicMdlList(uriModuleNm)

        for _, sonicMdlNm := range(sncMdlList) {
                sonicMdlTblInfo := xDbSpecTblSeqnMap[sonicMdlNm]
                for _, ordTblNm := range(sonicMdlTblInfo.OrdTbl) {
                                if xfmrTbl == ordTblNm {
                                        xfmrLogInfo("Found sonic module(%v) whose ordered table list contains table %v", sonicMdlNm, xfmrTbl)
                                        ordTblList = sonicMdlTblInfo.OrdTbl
                                        processedTbl = true
                                        break
                                }
                }
                if processedTbl {
                        break
                }
        }
		return ordTblList
	}

func GetXfmrOrdTblList(xfmrTbl string) []string {
	/* get the table hierarchy read from json file */
	var ordTblList []string
	if _, ok := sonicOrdTblListMap[xfmrTbl]; ok {
		ordTblList = sonicOrdTblListMap[xfmrTbl]
	}
	return ordTblList
}

func GetTablesToWatch(xfmrTblList []string, uriModuleNm string) []string {
        var depTblList []string
        depTblMap := make(map[string]bool) //create to avoid duplicates in depTblList, serves as a Set
        processedTbl := false
	var sncMdlList []string
	var lXfmrTblList []string

	sncMdlList = getYangMdlToSonicMdlList(uriModuleNm)

	// remove duplicates from incoming list of tables
	xfmrTblMap := make(map[string]bool) //create to avoid duplicates in xfmrTblList
	for _, xfmrTblNm :=range(xfmrTblList) {
		xfmrTblMap[xfmrTblNm] = true
	}
	for xfmrTblNm := range(xfmrTblMap) {
		lXfmrTblList = append(lXfmrTblList, xfmrTblNm)
	}

        for _, xfmrTbl := range(lXfmrTblList) {
		processedTbl = false
                //can be optimized if there is a way to know all sonic modules, a given OC-Yang spans over
                for _, sonicMdlNm := range(sncMdlList) {
                        sonicMdlTblInfo := xDbSpecTblSeqnMap[sonicMdlNm]
                        for _, ordTblNm := range(sonicMdlTblInfo.OrdTbl) {
                                if xfmrTbl == ordTblNm {
                                        xfmrLogInfo("Found sonic module(%v) whose ordered table list contains table %v", sonicMdlNm, xfmrTbl)
                                        ldepTblList := sonicMdlTblInfo.DepTbl[xfmrTbl]
                                        for _, depTblNm := range(ldepTblList) {
                                                depTblMap[depTblNm] = true
                                        }
                                        //assumption that a table belongs to only one sonic module
                                        processedTbl = true
                                        break
                                }
                        }
                        if processedTbl {
                                break
                        }
                }
		if !processedTbl {
			depTblMap[xfmrTbl] = false
		}
        }
        for depTbl := range(depTblMap) {
                depTblList = append(depTblList, depTbl)
        }
	return depTblList
}

func CallRpcMethod(path string, body []byte, dbs [db.MaxDB]*db.DB) ([]byte, error) {
	var err error
	var ret []byte
	var data []reflect.Value

	// TODO - check module name
	rpcName := strings.Split(path, ":")
	if dbXpathData, ok := xDbSpecMap[rpcName[1]]; ok {
		xfmrLogInfo("RPC callback invoked (%v) \r\n", rpcName)
		data, err = XlateFuncCall(dbXpathData.rpcFunc, body, dbs)
		if err != nil {
			return nil, err
		}
		ret = data[0].Interface().([]byte)
		if !data[1].IsNil() {
            err = data[1].Interface().(error)
        }
	} else {
		log.Error("No tsupported RPC", path)
		err = tlerr.NotSupported("Not supported RPC")
	}

	return ret, err
}

func AddModelCpbltInfo() map[string]*mdlInfo {
	return xMdlCpbltMap
}

func xfmrSubscSubtreeHandler(inParams XfmrSubscInParams, xfmrFuncNm string) (XfmrSubscOutParams, error) {
    var retVal XfmrSubscOutParams
    retVal.dbDataMap = nil
    retVal.needCache = false
    retVal.onChange = false
    retVal.nOpts = nil
    retVal.isVirtualTbl = false

    xfmrLogInfo("Received inParams %v Subscribe Subtree function name %v", inParams, xfmrFuncNm)
    ret, err := XlateFuncCall("Subscribe_"  + xfmrFuncNm, inParams)
    if err != nil {
        return retVal, err
    }

    if ((ret != nil) && (len(ret)>0)) {
        if len(ret) == SUBSC_SBT_XFMR_RET_ARGS {
            // subtree xfmr returns err as second value in return data list from <xfmr_func>.Call()
            if ret[SUBSC_SBT_XFMR_RET_ERR_INDX].Interface() != nil {
                err = ret[SUBSC_SBT_XFMR_RET_ERR_INDX].Interface().(error)
                if err != nil {
                    log.Warningf("Subscribe Transformer function(\"%v\") returned error - %v.", xfmrFuncNm, err)
                    return retVal, err
                }
            }
        }
        if ret[SUBSC_SBT_XFMR_RET_VAL_INDX].Interface() != nil {
            retVal = ret[SUBSC_SBT_XFMR_RET_VAL_INDX].Interface().(XfmrSubscOutParams)
        }
    }
    return retVal, err
}

func XlateTranslateSubscribe(path string, dbs [db.MaxDB]*db.DB, txCache interface{}) (XfmrTranslateSubscribeInfo, error) {
       xfmrLogInfo("Received subcription path : %v", path)
       var err error
       var subscribe_result XfmrTranslateSubscribeInfo
       subscribe_result.DbDataMap = make(RedisDbMap)
       subscribe_result.PType = Sample
       subscribe_result.MinInterval = 0
       subscribe_result.OnChange = false
       subscribe_result.NeedCache = true

       for {
           done := true
           xpath, predc_err := XfmrRemoveXPATHPredicates(path)
           if predc_err != nil {
               log.Errorf("cannot convert request Uri to yang xpath - %v, %v", path, predc_err)
               err = tlerr.NotSupportedError{Format: "Subscribe not supported", Path: path}
               break
           }
           xpathData, ok := xYangSpecMap[xpath]
           if ((!ok) || (xpathData == nil)) {
               log.Errorf("xYangSpecMap data not found for xpath : %v", xpath)
               err = tlerr.NotSupportedError{Format: "Subscribe not supported", Path: path}
               break
           }

           if (xpathData.subscribePref == nil || ((xpathData.subscribePref != nil) &&(len(strings.TrimSpace(*xpathData.subscribePref)) == 0))) {
               subscribe_result.PType = Sample
           } else {
               if *xpathData.subscribePref == "onchange" {
                   subscribe_result.PType = OnChange
               } else {
                           subscribe_result.PType = Sample
               }
           }
           subscribe_result.MinInterval = xpathData.subscribeMinIntvl

           if xpathData.subscribeOnChg == XFMR_DISABLE {
               xfmrLogInfo("Susbcribe OnChange disabled for request Uri - %v", path)
               subscribe_result.PType = Sample
               subscribe_result.DbDataMap = nil
               //err = tlerr.NotSupportedError{Format: "Subscribe not supported", Path: path}
               break
           }

           //request uri should be terminal yang object for onChange to be supported
           if xpathData.hasNonTerminalNode {
               xfmrLogInfo("Susbcribe request Uri is not a terminal yang object - %v", path)
               err = tlerr.NotSupportedError{Format: "Subscribe not supported", Path: path}
               break
           }

	   /*request uri is a key-leaf directly under the list
             eg. /openconfig-xyz:xyz/listA[key=value]/key
	         /openconfig-xyz:xyz/listA[key_1=value][key_2=value]/key_1
           */
	   if xpathData.isKey {
               xfmrLogInfo("Susbcribe request Uri is not a terminal yang object - %v", path)
               err = tlerr.NotSupportedError{Format: "Subscribe not supported", Path: path}
               break
	   }

           xpath_dbno := xpathData.dbIndex
           _, dbKey, dbTbl, xPathKeyExtractErr := xpathKeyExtract(dbs[xpath_dbno], nil, SUBSCRIBE, path, path, nil, txCache)
           if ((len(xpathData.xfmrFunc) == 0) && ((xPathKeyExtractErr != nil) || ((len(strings.TrimSpace(dbKey)) == 0) || (len(strings.TrimSpace(dbTbl)) == 0)))) {
               log.Error("Error while extracting DB table/key for uri", path, "error - ", xPathKeyExtractErr)
               err = xPathKeyExtractErr
               break
           }
           if (len(xpathData.xfmrFunc) > 0) { //subtree
               var inParams XfmrSubscInParams
               inParams.uri = path
               inParams.dbDataMap = subscribe_result.DbDataMap
               inParams.dbs = dbs
               inParams.subscProc = TRANSLATE_SUBSCRIBE
               st_result, st_err := xfmrSubscSubtreeHandler(inParams, xpathData.xfmrFunc)
               if st_err != nil {
                   err = st_err
                   break
               }
               if st_result.dbDataMap != nil {
                   subscribe_result.DbDataMap = st_result.dbDataMap
                   xfmrLogInfo("Subtree subcribe dbData %v", subscribe_result.DbDataMap)
               }
               if st_result.nOpts != nil {
                   subscribe_result.PType = st_result.nOpts.pType
                   xfmrLogInfo("Subtree subcribe pType %v", subscribe_result.PType)
                   subscribe_result.MinInterval = st_result.nOpts.mInterval
                   xfmrLogInfo("Subtree subcribe min interval %v", subscribe_result.MinInterval)
               }
               subscribe_result.OnChange = st_result.onChange
               xfmrLogInfo("Subtree subcribe on change %v", subscribe_result.OnChange)
               subscribe_result.NeedCache = st_result.needCache
               xfmrLogInfo("Subtree subcribe need Cache %v", subscribe_result.NeedCache)
           } else {
		   subscribe_result.OnChange = true
		   subscribe_result.DbDataMap[xpath_dbno] = map[string]map[string]db.Value{dbTbl: {dbKey: {}}}
	   }
           if done {
                   break
           }
       } // end of infinite for

       return subscribe_result, err

}

func IsTerminalNode(uri string) (bool, error) {
	xpath, err := XfmrRemoveXPATHPredicates(uri)
	if xpathData, ok := xYangSpecMap[xpath]; ok {
		if !xpathData.hasNonTerminalNode {
			return true, nil
		}
	} else {
		log.Errorf("xYangSpecMap data not found for xpath : %v", xpath)
		errStr := "xYangSpecMap data not found for xpath."
		log.Error(errStr)
		err = tlerr.InternalError{Format: errStr}
	}

	log.Errorf("xYangSpecMap data not found for xpath : %v", xpath)
	return false, err
}
