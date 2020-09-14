package custom_validation

import (
    log "github.com/golang/glog"
    "reflect"
    util "github.com/Azure/sonic-mgmt-common/cvl/internal/util"
 )

//ValidateDstPort validates whether destination port has any VLAN configuration
func (t *CustomValidation) ValidateDstPort(vc *CustValidationCtxt) CVLErrorInfo {

    if (vc.CurCfg.VOp == OP_DELETE) {
        return CVLErrorInfo{ErrCode: CVL_SUCCESS}
    }

    /* check if input passed is found in ConfigDB VLAN_MEMBER|* */
    tableKeys, err:= vc.RClient.Keys("VLAN_MEMBER|*|" + vc.YNodeVal).Result()
    if (err != nil) || (vc.SessCache == nil) {
        log.Error("Error reading VLAN_MEMBER Table")
        errStr := "Destination port validation failure"
        return CVLErrorInfo{
            ErrCode: CVL_SEMANTIC_ERROR,
            TableName: "VLAN_MEMBER",
            CVLErrDetails : errStr,
            ConstraintErrMsg : errStr,
        }
    }

    s := reflect.ValueOf(tableKeys)
    if (s.Len() > 0) {
        log.Error("ValidateDstPortVlanMember: ", vc.YNodeVal, " has vlans configuration: ", s.Len())
        errStr := "Destination port has VLAN config"
        return CVLErrorInfo{
            ErrCode: CVL_SEMANTIC_ERROR,
            TableName: "VLAN_MEMBER",
            CVLErrDetails : errStr,
            ConstraintErrMsg : errStr,
        }
    }

    log.Info("ValidateDstPortVlanMember: ", vc.YNodeVal, " has no vlan configuration: ", s.Len())

    /* Disabling LLDP validation until FT scripts are taken care 
    // check if LLDP is disabled on port
    lldpData, err1 := vc.RClient.HGetAll("LLDP_PORT|" + vc.YNodeVal).Result() 
    // By default LLDP is enabled
    if err1 != nil {
        log.Error("ValidateDstPort has LLDP enabled: ", vc.YNodeVal)
        errStr := "Destination port has LLDP enabled"
        return CVLErrorInfo{
            ErrCode: CVL_SEMANTIC_ERROR,
            TableName: "LLDP_PORT",
            CVLErrDetails : errStr,
            ConstraintErrMsg : errStr,
        }
    }

    if lldpData["enabled"] != "false" {
        log.Error("ValidateDstPort: ", vc.YNodeVal, " has LLDP: ", lldpData["enabled"])
        errStr := "Destination port has LLDP enabled"
        return CVLErrorInfo{
            ErrCode: CVL_SEMANTIC_ERROR,
            TableName: "LLDP_PORT",
            CVLErrDetails : errStr,
            ConstraintErrMsg : errStr,
        }
    }

    log.Info("ValidateDstPort: ", vc.YNodeVal, "has LLDP: ", lldpData["enabled"])
    */
    return CVLErrorInfo{ErrCode: CVL_SUCCESS}
}

//ValidateMaxMirrorSessions validates whether mirror sessions are available.
func (t *CustomValidation) ValidateMaxMirrorSessions(vc *CustValidationCtxt) CVLErrorInfo {

    if (vc.CurCfg.VOp == OP_DELETE) {
        return CVLErrorInfo{ErrCode: CVL_SUCCESS}
    }

    stateDBClient := util.NewDbClient("STATE_DB")
    defer func() {
        if (stateDBClient != nil) {
            stateDBClient.Close()
        }
    }()

    if (stateDBClient != nil) {
        key := "MIRROR_SESSION_TABLE|mirror_capability"
        available_count, err := stateDBClient.HGet(key, "available_count").Result()

        if (err == nil) {
            if (available_count == "0") {
                log.Error("ValidateMaxMirrorSessions: Exceed max active sessions.", available_count)
                errStr := "Maximum sessions already configured"
                return CVLErrorInfo{
                    ErrCode: CVL_SEMANTIC_ERROR,
                    TableName: "MIRROR_SESSION_TABLE",
                    CVLErrDetails : errStr,
                    ConstraintErrMsg : errStr,
                }
            }
        }
        log.Info("ValidateMaxMirrorSessions: available_sessions ", available_count)
    }
    return CVLErrorInfo{ErrCode: CVL_SUCCESS}
}
