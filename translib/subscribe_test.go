////////////////////////////////////////////////////////////////////////////////
//                                                                            //
//  Copyright 2021 Broadcom. The term Broadcom refers to Broadcom Inc. and/or //
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

package translib

import (
	"encoding/json"
	"fmt"
	"reflect"
	"testing"

	"github.com/Azure/sonic-mgmt-common/translib/db"
	"github.com/Azure/sonic-mgmt-common/translib/ocbinds"
	"github.com/openconfig/ygot/ygot"
)

var (
	roDBs [db.MaxDB]*db.DB
)

func getReadOnlyDB() [db.MaxDB]*db.DB {
	if roDBs[0] == nil {
		roDBs, _ = getAllDbs(true)
		addCleanupFunc(closeAllTestDB)
	}
	return roDBs
}

func closeAllTestDB() error {
	if roDBs[0] != nil {
		closeAllDbs(roDBs[:])
	}
	return nil
}

func Test_isEmptyStruct_EmptyObj(t *testing.T) {
	v := &ocbinds.OpenconfigAcl_Acl_AclSets_AclSet{}
	if !isEmptyYgotStruct(v) {
		t.FailNow()
	}
}

func Test_isEmptyStruct_DirectAttr(t *testing.T) {
	x := &ocbinds.OpenconfigAcl_Acl_AclSets_AclSet{
		Name: ygot.String("Foo"),
	}
	if isEmptyYgotStruct(x) {
		t.FailNow()
	}
}

func Test_isEmptyStruct_NestedAttr(t *testing.T) {
	x := new(ocbinds.OpenconfigAcl_Acl_AclSets_AclSet)
	ygot.BuildEmptyTree(x)
	x.Config.Description = ygot.String("Hello, world!")
	if isEmptyYgotStruct(x) {
		t.FailNow()
	}
}

func Test_isEmptyStruct_EmptyContainers(t *testing.T) {
	x := new(ocbinds.OpenconfigAcl_Acl_AclSets_AclSet)
	ygot.BuildEmptyTree(x)
	if !isEmptyYgotStruct(x) {
		t.FailNow()
	}
}

func Test_isEmptyStruct_EmptyTree(t *testing.T) {
	x := new(ocbinds.OpenconfigAcl_Acl_AclSets_AclSet_AclEntries_AclEntry)
	ygot.BuildEmptyTree(x)
	if !isEmptyYgotStruct(x) {
		t.FailNow()
	}
}

func Test_isEmptyStruct_NDeepAttr(t *testing.T) {
	x := new(ocbinds.OpenconfigAcl_Acl_AclSets_AclSet_AclEntries_AclEntry)
	ygot.BuildEmptyTree(x)
	x.Ipv4.Config.SourceAddress = ygot.String("1.2.3.4/32")
	if isEmptyYgotStruct(x) {
		t.FailNow()
	}
}

func Test_isEmptyStruct_NDeepLeafList(t *testing.T) {
	x := new(ocbinds.OpenconfigAcl_Acl_AclSets_AclSet_AclEntries_AclEntry)
	ygot.BuildEmptyTree(x)
	x.Transport.Config.TcpFlags = []ocbinds.E_OpenconfigPacketMatchTypes_TCP_FLAGS{ocbinds.OpenconfigPacketMatchTypes_TCP_FLAGS_TCP_ACK}
	if isEmptyYgotStruct(x) {
		t.FailNow()
	}
}

func Test_isEmptyStruct_NDeepList(t *testing.T) {
	x := new(ocbinds.OpenconfigAcl_Acl_AclSets_AclSet)
	ygot.BuildEmptyTree(x)
	x.AclEntries.NewAclEntry(10)
	if isEmptyYgotStruct(x) {
		t.FailNow()
	}
}

///////////////////

// Messages is a utility to collect list of
// formatted messages and log it later.
type Messages struct {
	list []string
}

func (m *Messages) Add(format string, args ...interface{}) {
	m.list = append(m.list, fmt.Sprintf(format, args...))
}

func (m *Messages) Empty() bool {
	return len(m.list) == 0
}

func (m *Messages) LogTo(t *testing.T) {
	for _, s := range m.list {
		t.Log(s)
	}
}

///////////////////
// Utilities to test translateSubscribe

var translErr = -1

type translateSubscribeVerifier struct {
	t           *testing.T
	path        string
	mode        NotificationType
	targetInfos []*notificationAppInfo
	childInfos  []*notificationAppInfo
	appError    error
}

func testTranslateSubscribe(t *testing.T, path string, mode NotificationType) *translateSubscribeVerifier {
	tv := &translateSubscribeVerifier{
		t:    t,
		path: path,
		mode: mode,
	}
	sc := subscribeContext{
		id:   subscribeCounter.Next(),
		dbs:  getReadOnlyDB(),
		mode: mode,
	}
	resp, _, err := sc.translatePath(path)
	if err != nil {
		tv.appError = err
	} else {
		tv.targetInfos = resp.ntfAppInfoTrgt
		tv.childInfos = resp.ntfAppInfoTrgtChlds
	}

	return tv
}

// VerifyCount validates if translateSubscribe returned expected number of
// target and child notificationAppInfo objects. Pass any of the count as translErr
// to validate for error response.
func (tv *translateSubscribeVerifier) VerifyCount(targetCount, childCount int) {
	if targetCount == translErr || childCount == translErr {
		if tv.appError == nil {
			tv.t.Fatalf("tanslateSubscribe(%s, %s) should have failed", tv.path, tv.mode)
		}
		return
	}
	if tv.appError != nil {
		tv.t.Fatalf("tanslateSubscribe(%s, %s) failed; err=%v", tv.path, tv.mode, tv.appError)
	}
	if len(tv.targetInfos) != targetCount && len(tv.childInfos) != childCount {
		tv.t.Fatalf("translateSubscribe(%s, %s) failed; Expected %v target and %d child infos. Found %d and %d",
			tv.path, tv.mode, targetCount, childCount, len(tv.targetInfos), len(tv.childInfos))
	}
}

// VerifyTarget checks if target notificationAppInfo list has a matching entry
func (tv *translateSubscribeVerifier) VerifyTarget(path string, expInfo *notificationAppInfo) {
	nInfo := tv.find(path, tv.targetInfos)
	if nInfo == nil {
		tv.t.Fatalf("Did not find targetInfo for '%s'", path)
	}
	tv.compare(nInfo, expInfo)
}

// VerifyChild checks if child notificationAppInfo list has a matching entry
func (tv *translateSubscribeVerifier) VerifyChild(path string, nAppInfo *notificationAppInfo) {
	nInfo := tv.find(path, tv.targetInfos)
	if nInfo == nil {
		tv.t.Fatalf("Did not find childInfo for '%s'", path)
	}
	tv.compare(nInfo, nAppInfo)
}

func (tv *translateSubscribeVerifier) find(path string, list []*notificationAppInfo) *notificationAppInfo {
	for _, nInfo := range list {
		if p, err := ygot.PathToString(nInfo.path); err != nil {
			tv.t.Errorf("translateSubscribe(%s, %s) returned invalid appInfo path: %v; err=%v",
				tv.path, tv.mode, nInfo.path, err)
			return nil
		} else if p == path {
			return nInfo
		}
	}
	return nil
}

func (tv *translateSubscribeVerifier) compare(nInfo, expInfo *notificationAppInfo) {
	var errors Messages
	if nInfo.dbno != expInfo.dbno {
		errors.Add("dbno mismatch; expected=%v, found=%v", expInfo.dbno, nInfo.dbno)
	}
	if tableInfo(nInfo.table) != tableInfo(expInfo.table) {
		errors.Add("table mismatch; expected=%v, found=%v", tableInfo(expInfo.table), tableInfo(nInfo.table))
	}
	if expInfo.key != nil && (nInfo.key == nil || !expInfo.key.Equals(nInfo.key)) {
		errors.Add("key mismatch; expected=%v, found=%v", keyInfo(expInfo.key), keyInfo(nInfo.key))
	}
	dbFields := nInfo.fieldsJSON()
	expFields := expInfo.fieldsJSON()
	if !reflect.DeepEqual(dbFields, expFields) {
		val, _ := json.Marshal(dbFields)
		exp, _ := json.Marshal(expFields)
		errors.Add("dbFldYgPathInfoList mismatch;")
		errors.Add("  expected=%v", string(exp))
		errors.Add("  found=%v", string(val))
	}
	if nInfo.isPartial != expInfo.isPartial {
		errors.Add("isPartial mismatch; expected=%v, found=%v", expInfo.isPartial, nInfo.isPartial)
	}
	if tv.mode != Sample && nInfo.isOnChangeSupported != expInfo.isOnChangeSupported {
		errors.Add("isOnChangeSupported mismatch; expected=%v, found=%v", expInfo.isOnChangeSupported, nInfo.isOnChangeSupported)
	}
	if tv.mode != OnChange && nInfo.mInterval != expInfo.mInterval {
		errors.Add("minInterval mismatch; expected=%v, found=%v", expInfo.mInterval, nInfo.mInterval)
	}
	if tv.mode == TargetDefined && nInfo.pType != expInfo.pType {
		errors.Add("preferredMode mismatch; expected=%v, found=%v", expInfo.pType, nInfo.pType)
	}
	if !errors.Empty() {
		p, _ := ygot.PathToString(nInfo.path)
		tv.t.Errorf("translateSubscribe(%s, %s) returned invalid data", tv.path, tv.mode)
		tv.t.Errorf("notificationAppInfo for '%s' does not match expected values", p)
		errors.LogTo(tv.t)
	}
}

///////////////////
// notificationAppInfo customizations

// subscribeFieldsJSON represents dbFldYgPathInfo objects in JSON format.
// Syntax: {"prefix1": {"db_field1": "yang_field1", ...}, "prefix2": {...}}
type subscribeFieldsJSON map[string]map[string]string

// fieldsJSON returns ni.dbFldYgPathInfoList as a subscribeFieldsJSON object.
func (ni *notificationAppInfo) fieldsJSON() subscribeFieldsJSON {
	jsonData := make(subscribeFieldsJSON)
	for _, entry := range ni.dbFldYgPathInfoList {
		jsonData[entry.rltvPath] = entry.dbFldYgPathMap
	}
	return jsonData
}

// setFields updates ni.dbFldYgPathInfoList from a JSON string
// in subscribeFieldsJSON syntax.
func (ni *notificationAppInfo) setFields(fieldsJSON string) {
	ni.dbFldYgPathInfoList = parseFieldsJSON(fieldsJSON)
}

// parseFieldsJSON parses a JSON string in subscribeFieldsJSON syntax into
// an array of dbFldYgPathInfo objects.
func parseFieldsJSON(mappingJSON string) []*dbFldYgPathInfo {
	jsonData := make(subscribeFieldsJSON)
	err := json.Unmarshal([]byte(mappingJSON), &jsonData)
	if err != nil {
		panic(fmt.Sprintf("json.Unmarshal failed; err=%v; json=%v", err, mappingJSON))
	}
	var mappings []*dbFldYgPathInfo
	for prefix, fields := range jsonData {
		mappings = append(mappings, &dbFldYgPathInfo{rltvPath: prefix, dbFldYgPathMap: fields})
	}
	return mappings
}
