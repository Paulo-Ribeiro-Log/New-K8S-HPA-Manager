package azure

import "testing"

// Formatos capturados de uma listagem real (RG de dados): estado Reserved = disco de VM desalocada.
func TestParseRGDisks_StateAndAttachment(t *testing.T) {
	body := []byte(`{"value":[
	 {"id":"/subscriptions/s/resourceGroups/rg/providers/Microsoft.Compute/disks/d1","name":"d1","location":"brazilsouth",
	  "managedBy":"/subscriptions/s/resourceGroups/RG/providers/Microsoft.Compute/virtualMachines/vm-a",
	  "sku":{"name":"Premium_LRS","tier":"Premium"},
	  "properties":{"diskState":"Attached","diskSizeGB":512,"diskIOPSReadWrite":2300,"diskMBpsReadWrite":150,"timeCreated":"2025-01-01T00:00:00Z"}},
	 {"id":"x/d2","name":"d2","location":"brazilsouth","managedBy":"/subscriptions/s/resourceGroups/RG/providers/Microsoft.Compute/virtualMachines/vm-stopped",
	  "sku":{"name":"StandardSSD_LRS","tier":"Standard"},"properties":{"diskState":"Reserved","diskSizeGB":128}},
	 {"id":"x/d3","name":"d3","location":"brazilsouth","sku":{"name":"Standard_LRS"},"properties":{"diskState":"Unattached","diskSizeGB":64,"tier":"S6"}}
	],"nextLink":"https://management.azure.com/next"}`)
	disks, next, err := parseRGDisks(body)
	if err != nil || len(disks) != 3 || next == "" {
		t.Fatalf("parse: %v %d %q", err, len(disks), next)
	}
	if d := disks[0]; d.State != "Attached" || d.AttachedVMName() != "vm-a" || d.SKUTier != "Premium" || d.IOPS != 2300 || d.SizeGB != 512 {
		t.Errorf("d1: %+v", d)
	}
	if d := disks[1]; d.State != "Reserved" || d.AttachedVMName() != "vm-stopped" {
		t.Errorf("d2 (VM desalocada): %+v", d)
	}
	if d := disks[2]; d.State != "Unattached" || d.AttachedVMName() != "" || d.PerformanceTier != "S6" {
		t.Errorf("d3 (desatachado, com tier): %+v", d)
	}
}

func TestParseRGVMsAndResources(t *testing.T) {
	vms, _, err := parseRGVMs([]byte(`{"value":[{"id":"i","name":"vm-a","location":"brazilsouth","properties":{"hardwareProfile":{"vmSize":"Standard_D2s_v4"}}}]}`))
	if err != nil || len(vms) != 1 || vms[0].VMSize != "Standard_D2s_v4" {
		t.Errorf("VMs: %v %+v", err, vms)
	}
	res, _, err := parseRGResources([]byte(`{"value":[
	  {"id":"i1","name":"pg","type":"Microsoft.DBforPostgreSQL/flexibleServers","location":"brazilsouth","sku":{"name":"Standard_D2ds_v5","tier":"GeneralPurpose"}},
	  {"id":"i2","name":"nic","type":"Microsoft.Network/networkInterfaces","location":"brazilsouth","sku":null}]}`))
	if err != nil || len(res) != 2 || res[0].SKUName != "Standard_D2ds_v5" || res[0].SKUTier != "GeneralPurpose" || res[1].SKUName != "" {
		t.Errorf("recursos: %v %+v", err, res)
	}
}

func TestParseMetricSummaries(t *testing.T) {
	body := []byte(`{"value":[
	  {"name":{"value":"Percentage CPU"},"timeseries":[{"data":[
	     {"average":2,"maximum":10},{"average":4,"maximum":75},{"average":6,"maximum":12},{"maximum":1},{"average":8,"maximum":9}]}]},
	  {"name":{"value":"sem_dado"},"timeseries":[{"data":[{}]}]}]}`)
	m, err := parseMetricSummaries(body)
	if err != nil {
		t.Fatal(err)
	}
	cpu, ok := m["Percentage CPU"]
	if !ok || cpu.Points != 4 || cpu.Avg != 5 || cpu.Max != 75 || cpu.P95 != 6 {
		t.Errorf("CPU: %+v", cpu)
	}
	if _, has := m["sem_dado"]; has {
		t.Error("métrica sem pontos não pode aparecer (nunca um zero inventado)")
	}
}

// Formatos capturados de servidores reais: PostgreSQL traz o tier de storage (P20...), MySQL não.
func TestParseRGFlexServers_PostgresAndMySQL(t *testing.T) {
	pg, _, err := parseRGFlexServers([]byte(`{"value":[{"id":"/s/pg","name":"pgsh-adanalytics-1","type":"Microsoft.DBforPostgreSQL/flexibleServers","location":"brazilsouth",
	  "sku":{"name":"Standard_B1ms","tier":"Burstable"},
	  "properties":{"storage":{"autoGrow":"Disabled","iops":2300,"storageSizeGB":512,"tier":"P20","type":"Premium_LRS"},
	   "highAvailability":{"mode":"Disabled","state":"NotEnabled"},"backup":{"backupRetentionDays":7,"geoRedundantBackup":"Disabled"}}}]}`))
	if err != nil || len(pg) != 1 {
		t.Fatalf("pg: %v %v", err, pg)
	}
	if s := pg[0]; s.StorageGB != 512 || s.StorageTier != "P20" || s.IOPS != 2300 || s.AutoGrow || s.HAEnabled() || s.SKUTier != "Burstable" {
		t.Errorf("PostgreSQL: %+v", s)
	}

	my, _, err := parseRGFlexServers([]byte(`{"value":[{"id":"/s/my","name":"mysp-xlrelease-1","type":"Microsoft.DBforMySQL/flexibleServers","location":"brazilsouth",
	  "sku":{"name":"Standard_D8ds_v4","tier":"GeneralPurpose"},
	  "properties":{"storage":{"autoGrow":"Enabled","autoIoScaling":"Enabled","iops":1758,"storageSizeGB":486,"storageSku":"Premium_LRS"},
	   "highAvailability":{"mode":"ZoneRedundant"},"backup":{"backupRetentionDays":14,"geoRedundantBackup":"Enabled"}}}]}`))
	if err != nil || len(my) != 1 {
		t.Fatalf("mysql: %v %v", err, my)
	}
	if s := my[0]; s.StorageGB != 486 || s.StorageTier != "" || !s.AutoGrow || !s.HAEnabled() || !s.GeoRedundantBackup || s.BackupRetentionDays != 14 {
		t.Errorf("MySQL: %+v", s)
	}
}
