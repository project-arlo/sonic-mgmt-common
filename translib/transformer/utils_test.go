////////////////////////////////////////////////////////////////////////////////
//                                                                            //
//  Copyright 2019 Broadcom. The term Broadcom refers to Broadcom Inc. and/or //
//  its subsidiaries.                                                         //
//                                                                            //
//  Licensed under the Apache License, Version 2.0 (the "License");           //
//  you may not use this file except in compliance with the License.          //
//  You may obtain a copy of the License at                                   //
//                                                                            //
//     http://www.apache.org/licenses/LICENSE-2.0                             //
//                                                                            //
//  Unless required by applicable law or agreed to in writing, software       //
//  distributed under the License is distributed on an "AS IS" BASIS,         //
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.  //
//  See the License for the specific language governing permissions and       //
//  limitations under the License.                                            //
//                                                                            //
////////////////////////////////////////////////////////////////////////////////

package transformer_test

import (
	"github.com/go-redis/redis/v7"
	"encoding/json"
	"io/ioutil"
	"fmt"
	"testing"
	"sync"
	"reflect"
	db "github.com/Azure/sonic-mgmt-common/translib/db"
	"github.com/Azure/sonic-mgmt-common/translib/transformer"
	. "github.com/Azure/sonic-mgmt-common/translib"
)



func checkErr(t *testing.T, err error, expErr error) {
	if err.Error() != expErr.Error() {
		t.Errorf("Error %v, Expect Err: %v", err, expErr)
	} else if reflect.TypeOf(err) != reflect.TypeOf(expErr) {
		t.Errorf("Error type %T, Expect Err Type: %T", err, expErr)
	}
}

func processGetRequest(url string, expectedRespJson string, errorCase bool, expErr ...error) func(*testing.T) {
	return func(t *testing.T) {
		var expectedMap map[string]interface{}
		var receivedMap map[string]interface{}
		response, err := Get(GetRequest{Path: url, User: UserRoles{Name: "admin", Roles: []string{"admin"}}})
		if err != nil {
			if !errorCase {
				t.Errorf("Error %v received for Url: %s", err, url)
			} else if expErr != nil {
				checkErr(t, err, expErr[0])
			}
		}

		err = json.Unmarshal([]byte(expectedRespJson), &expectedMap)
		if err != nil {
			t.Errorf("failed to unmarshal %v err: %v", expectedRespJson, err)
		}

		respJson := response.Payload
		err = json.Unmarshal([]byte(respJson), &receivedMap)
		if err != nil {
			t.Errorf("failed to unmarshal %v err: %v", string(respJson), err)
		}

		if reflect.DeepEqual(receivedMap, expectedMap) != true {
			t.Errorf("Response for Url: %s received is not expected:\n Received: %s\n Expected: %s", url, receivedMap, expectedMap)
		}
	}
}

func processGetRequestWithFile(url string, expectedJsonFile string, errorCase bool, expErr ...error) func(*testing.T) {
	return func(t *testing.T) {
		var expectedMap map[string]interface{}
		var receivedMap map[string]interface{}
		jsonStr, err := ioutil.ReadFile(expectedJsonFile)
		if err != nil {
			t.Errorf("read file %v err: %v", expectedJsonFile, err)
		}
		err = json.Unmarshal([]byte(jsonStr), &expectedMap)
		if err != nil {
			t.Errorf("failed to unmarshal %v err: %v", jsonStr, err)
		}

		response, err := Get(GetRequest{Path: url, User: UserRoles{Name: "admin", Roles: []string{"admin"}}})
		if err != nil {
			if !errorCase {
				t.Errorf("Error %v received for Url: %s", err, url)
			} else if expErr != nil {
				checkErr(t, err, expErr[0])
			}
		}

		respJson := response.Payload
		err = json.Unmarshal([]byte(respJson), &receivedMap)
		if err != nil {
			t.Errorf("failed to unmarshal %v err: %v", string(respJson), err)
		}

		if reflect.DeepEqual(receivedMap, expectedMap) != true {
			t.Errorf("Response for Url: %s received is not expected:\n Received: %s\n Expected: %s", url, receivedMap, expectedMap)
		}
	}
}

func processSetRequest(url string, jsonPayload string, oper string, errorCase bool, expErr ...error) func(*testing.T) {
	return func(t *testing.T) {
		var err error
		switch oper {
		case "POST":
			_, err = Create(SetRequest{Path: url, Payload: []byte(jsonPayload)})
		case "PATCH":
			_, err = Update(SetRequest{Path: url, Payload: []byte(jsonPayload)})
		case "PUT":
			_, err = Replace(SetRequest{Path: url, Payload: []byte(jsonPayload)})
		default:
			t.Errorf("Operation not supported")
		}
		if err != nil {
			if !errorCase {
				t.Errorf("Error %v received for Url: %s", err, url)
			} else if expErr != nil {
				checkErr(t, err, expErr[0])
			}
		}
	}
}

func processSetRequestFromFile(url string, jsonFile string, oper string, errorCase bool, expErr ...error) func(*testing.T) {
	return func(t *testing.T) {
		jsonPayload, err := ioutil.ReadFile(jsonFile)
		if err != nil {
			t.Errorf("read file %v err: %v", jsonFile, err)
		}
		switch oper {
		case "POST":
			_, err = Create(SetRequest{Path: url, Payload: []byte(jsonPayload)})
		case "PATCH":
			_, err = Update(SetRequest{Path: url, Payload: []byte(jsonPayload)})
		case "PUT":
			_, err = Replace(SetRequest{Path: url, Payload: []byte(jsonPayload)})
		default:
			t.Errorf("Operation not supported")
		}
		if err != nil {
			if !errorCase {
				t.Errorf("Error %v received for Url: %s", err, url)
			} else if expErr != nil {
				checkErr(t, err, expErr[0])
			}
		}
	}
}

func processDeleteRequest(url string, errorCase bool, expErr ...error) func(*testing.T) {
	return func(t *testing.T) {
		_, err := Delete(SetRequest{Path: url})
		if err != nil {
			if !errorCase {
				t.Errorf("Error %v received for Url: %s", err, url)
			} else if expErr != nil {
				checkErr(t, err, expErr[0])
			}
		}
	}
}

func translateSubscribeRequest(path string, expectedTrSubInfo transformer.XfmrTranslateSubscribeInfo, errorCase bool, expErr ...error) func(*testing.T) {
        return func(t *testing.T) {
        isGetCase := true
        dbs, err := getAllDbs(isGetCase)
        txCache := new(sync.Map)

        result, err := transformer.XlateTranslateSubscribe(path ,dbs, txCache)

		if err != nil {
			if errorCase == false {
				t.Errorf("Unexpected error processing '%s'; err=%v", path, err)
			} else if expErr != nil {
                                checkErr(t, err, expErr[0])
				return
                        }

		} else if ((err == nil) && errorCase) {
			t.Errorf("Expecting error but not received while processing '%s'; err=%v", path, err)
		}
		if result.PType != expectedTrSubInfo.PType {
			t.Errorf("PType mismatch : received %v, expected %v, for url - %v", result.PType, expectedTrSubInfo.PType, path)
		}
		if result.NeedCache != expectedTrSubInfo.NeedCache {
			t.Errorf("NeedCache mismatch : received %v, expected %v, for url - %v", result.NeedCache, expectedTrSubInfo.NeedCache, path)
		}
		if result.MinInterval != expectedTrSubInfo.MinInterval {
			t.Errorf("MinInterval mismatch : received %v, expected %v, for url - %v", result.MinInterval, expectedTrSubInfo.MinInterval, path)
		}
		if ((result.DbDataMap == nil) && (expectedTrSubInfo.DbDataMap != nil)) {
			t.Errorf("DB Info mismatch : \nreceived nil, \nexpected %v, \nfor url - %v", expectedTrSubInfo.DbDataMap, path)
		}
        if ((result.DbDataMap != nil) && (expectedTrSubInfo.DbDataMap != nil)) {
		    if reflect.DeepEqual(result.DbDataMap, expectedTrSubInfo.DbDataMap) != true {
                            t.Errorf("DB Info mismatch : \nreceived - %v, \nexpected - %v, \n for url - %v", result.DbDataMap, expectedTrSubInfo.DbDataMap, path)
                    }
        }
	}

}

func processActionRequest(url string, jsonPayload string, oper string, errorCase bool, expErr ...error) func(*testing.T) {
	return func(t *testing.T) {
		var err error
		switch oper {
		case "POST":
			_, err = Action(ActionRequest{Path: url, Payload: []byte(jsonPayload)})
		default:
			t.Errorf("Operation not supported")
		}
		if err != nil {
			if !errorCase {
				t.Errorf("Error %v received for Url: %s", err, url)
			} else if expErr != nil {
				checkErr(t, err, expErr[0])
			}
		}
	}
}

func getConfigDb() *db.DB {
	configDb, _ := db.NewDB(db.Options{
		DBNo:               db.ConfigDB,
		InitIndicator:      "CONFIG_DB_INITIALIZED",
		TableNameSeparator: "|",
		KeySeparator:       "|",
	})

	return configDb
}

func verifyDbResult(client *redis.Client, key string, expectedResult map[string]interface{}, errorCase bool) func(*testing.T) {
	return func(t *testing.T) {
		result, err := client.HGetAll(key).Result()
		if err != nil {
			t.Errorf("Error %v hgetall for key: %s", err, key)
		}

		expect := make(map[string]string)
		for ts := range expectedResult {
			for _,k := range expectedResult[ts].(map[string]interface{}) {
				for f,v := range k.(map[string]interface{}) {
					strKey := fmt.Sprintf("%v", f)
					var strVal string
					strVal = fmt.Sprintf("%v", v)
					expect[strKey] = strVal
				}
			}
		}

		if reflect.DeepEqual(result, expect) != true {
			t.Errorf("Response for Key: %v received is not expected: Received %v Expected %v\n", key, result, expect)
		}
	}
}

var emptyJson string = "{}"
