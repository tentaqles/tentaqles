package model

import "strings"

// Stateful resource types (spec D §7.2 IAC001): databases, buckets and
// storage accounts, key vaults and KMS keys, disks and volumes, DNS zones,
// Cosmos and Dynamo tables, plus the caches, warehouses, file systems and
// secret stores whose loss is data loss. One table for every provider, used
// by IAC001 and by the plan summary.

var statefulExact = map[string]bool{}

// statefulPrefix entries end in "/" or ":" and match a type family.
var statefulPrefix []string

func init() {
	for _, t := range []string{
		// Terraform aws
		"aws_db_instance", "aws_rds_cluster", "aws_rds_cluster_instance", "aws_rds_global_cluster",
		"aws_dynamodb_table", "aws_dynamodb_global_table", "aws_s3_bucket", "aws_efs_file_system",
		"aws_ebs_volume", "aws_kms_key", "aws_route53_zone", "aws_elasticache_cluster",
		"aws_elasticache_replication_group", "aws_redshift_cluster", "aws_docdb_cluster",
		"aws_neptune_cluster", "aws_opensearch_domain", "aws_elasticsearch_domain",
		"aws_secretsmanager_secret", "aws_backup_vault", "aws_glacier_vault", "aws_memorydb_cluster",
		"aws_fsx_lustre_file_system", "aws_fsx_windows_file_system", "aws_fsx_ontap_file_system",
		"aws_fsx_openzfs_file_system", "aws_timestreamwrite_database", "aws_keyspaces_table",
		// Terraform azurerm
		"azurerm_mssql_server", "azurerm_mssql_database", "azurerm_mssql_managed_instance",
		"azurerm_sql_server", "azurerm_sql_database", "azurerm_postgresql_server",
		"azurerm_postgresql_database", "azurerm_postgresql_flexible_server",
		"azurerm_postgresql_flexible_server_database", "azurerm_mysql_server", "azurerm_mysql_database",
		"azurerm_mysql_flexible_server", "azurerm_mysql_flexible_database", "azurerm_cosmosdb_account",
		"azurerm_cosmosdb_sql_database", "azurerm_cosmosdb_sql_container", "azurerm_cosmosdb_mongo_database",
		"azurerm_storage_account", "azurerm_storage_container", "azurerm_storage_share",
		"azurerm_key_vault", "azurerm_key_vault_key", "azurerm_managed_disk", "azurerm_dns_zone",
		"azurerm_private_dns_zone", "azurerm_redis_cache", "azurerm_recovery_services_vault",
		"azurerm_data_protection_backup_vault", "azurerm_storage_data_lake_gen2_filesystem",
		"azurerm_key_vault_secret", "azurerm_key_vault_certificate", "azurerm_storage_table",
		"azurerm_storage_queue", "azurerm_cosmosdb_mongo_collection", "azurerm_cosmosdb_cassandra_keyspace",
		"azurerm_cosmosdb_cassandra_table", "azurerm_cosmosdb_gremlin_database", "azurerm_cosmosdb_gremlin_graph",
		"azurerm_cosmosdb_table",
		// Terraform google
		"google_sql_database_instance", "google_sql_database", "google_storage_bucket",
		"google_kms_key_ring", "google_kms_crypto_key", "google_compute_disk",
		"google_compute_region_disk", "google_dns_managed_zone", "google_bigquery_dataset",
		"google_bigquery_table", "google_spanner_instance", "google_spanner_database",
		"google_bigtable_instance", "google_bigtable_table", "google_firestore_database",
		"google_redis_instance", "google_filestore_instance", "google_alloydb_cluster",
		"google_secret_manager_secret",
		// CloudFormation
		"AWS::RDS::DBInstance", "AWS::RDS::DBCluster", "AWS::RDS::GlobalCluster",
		"AWS::DynamoDB::Table", "AWS::DynamoDB::GlobalTable", "AWS::S3::Bucket",
		"AWS::EFS::FileSystem", "AWS::EC2::Volume", "AWS::KMS::Key", "AWS::Route53::HostedZone",
		"AWS::ElastiCache::CacheCluster", "AWS::ElastiCache::ReplicationGroup",
		"AWS::Redshift::Cluster", "AWS::DocDB::DBCluster", "AWS::Neptune::DBCluster",
		"AWS::OpenSearchService::Domain", "AWS::Elasticsearch::Domain",
		"AWS::SecretsManager::Secret", "AWS::Backup::BackupVault", "AWS::FSx::FileSystem",
		"AWS::MemoryDB::Cluster", "AWS::Cassandra::Table", "AWS::Timestream::Database",
		"AWS::Serverless::SimpleTable",
	} {
		statefulExact[t] = true
	}
	for _, t := range []string{
		// ARM (matched without regard to case; see Stateful)
		"Microsoft.Sql/servers", "Microsoft.Sql/servers/databases", "Microsoft.Sql/managedInstances",
		"Microsoft.DBforPostgreSQL/servers", "Microsoft.DBforPostgreSQL/flexibleServers",
		"Microsoft.DBforPostgreSQL/servers/databases", "Microsoft.DBforPostgreSQL/flexibleServers/databases",
		"Microsoft.DBforMySQL/servers", "Microsoft.DBforMySQL/flexibleServers",
		"Microsoft.DBforMySQL/servers/databases", "Microsoft.DBforMySQL/flexibleServers/databases",
		"Microsoft.DocumentDB/databaseAccounts", "Microsoft.Storage/storageAccounts",
		"Microsoft.Storage/storageAccounts/blobServices/containers",
		"Microsoft.Storage/storageAccounts/fileServices/shares", "Microsoft.KeyVault/vaults",
		"Microsoft.KeyVault/vaults/keys", "Microsoft.KeyVault/managedHSMs", "Microsoft.Compute/disks",
		"Microsoft.Network/dnsZones", "Microsoft.Network/privateDnsZones", "Microsoft.Cache/redis",
		"Microsoft.Cache/redisEnterprise", "Microsoft.RecoveryServices/vaults",
		"Microsoft.DataProtection/backupVaults", "Microsoft.KeyVault/vaults/secrets",
		"Microsoft.DocumentDB/databaseAccounts/sqlDatabases",
		"Microsoft.DocumentDB/databaseAccounts/sqlDatabases/containers",
		"Microsoft.DocumentDB/databaseAccounts/mongodbDatabases",
		"Microsoft.DocumentDB/databaseAccounts/mongodbDatabases/collections",
		"Microsoft.DocumentDB/databaseAccounts/cassandraKeyspaces",
		"Microsoft.DocumentDB/databaseAccounts/cassandraKeyspaces/tables",
		"Microsoft.DocumentDB/databaseAccounts/gremlinDatabases",
		"Microsoft.DocumentDB/databaseAccounts/gremlinDatabases/graphs",
		"Microsoft.DocumentDB/databaseAccounts/tables",
		"Microsoft.Storage/storageAccounts/tableServices/tables",
		"Microsoft.Storage/storageAccounts/queueServices/queues",
	} {
		statefulExact[strings.ToLower(t)] = true
	}
	statefulPrefix = []string{
		// Pulumi classic providers (tokens are <pkg>:<module>/<name>:<Type>)
		"aws:rds/instance:", "aws:rds/cluster:", "aws:rds/clusterinstance:", "aws:rds/globalcluster:",
		"aws:dynamodb/table:", "aws:s3/bucket:", "aws:s3/bucketv2:", "aws:efs/filesystem:",
		"aws:ebs/volume:", "aws:kms/key:", "aws:route53/zone:", "aws:elasticache/cluster:",
		"aws:elasticache/replicationgroup:", "aws:redshift/cluster:", "aws:docdb/cluster:",
		"aws:neptune/cluster:", "aws:opensearch/domain:", "aws:elasticsearch/domain:",
		"aws:secretsmanager/secret:", "aws:backup/vault:", "aws:memorydb/cluster:",
		"azure:mssql/server:", "azure:mssql/database:", "azure:postgresql/server:", "azure:postgresql/flexibleserver:", "azure:postgresql/database:",
		"azure:postgresql/flexibleserverdatabase:", "azure:mysql/server:", "azure:mysql/flexibleserver:",
		"azure:mysql/database:", "azure:mysql/flexibledatabase:",
		"azure:cosmosdb/account:", "azure:storage/account:", "azure:storage/container:",
		"azure:storage/share:", "azure:keyvault/keyvault:", "azure:keyvault/key:",
		"azure:compute/manageddisk:", "azure:dns/zone:", "azure:privatedns/zone:", "azure:redis/cache:",
		"azure:keyvault/secret:", "azure:keyvault/certificate:", "azure:storage/table:", "azure:storage/queue:",
		"azure:cosmosdb/sqldatabase:", "azure:cosmosdb/sqlcontainer:", "azure:cosmosdb/mongodatabase:",
		"azure:cosmosdb/mongocollection:", "azure:cosmosdb/cassandrakeyspace:", "azure:cosmosdb/cassandratable:",
		"azure:cosmosdb/gremlindatabase:", "azure:cosmosdb/gremlingraph:", "azure:cosmosdb/table:",
		"gcp:sql/databaseinstance:", "gcp:sql/database:", "gcp:storage/bucket:", "gcp:kms/keyring:",
		"gcp:kms/cryptokey:", "gcp:compute/disk:", "gcp:compute/regiondisk:", "gcp:dns/managedzone:",
		"gcp:bigquery/dataset:", "gcp:bigquery/table:", "gcp:spanner/instance:", "gcp:spanner/database:", "gcp:bigtable/instance:", "gcp:bigtable/table:",
		"gcp:firestore/database:", "gcp:redis/instance:", "gcp:filestore/instance:",
		// Pulumi native providers (<pkg>:<module>:<Type>)
		"azure-native:sql:server", "azure-native:sql:database", "azure-native:sql:managedinstance",
		"azure-native:dbforpostgresql:server", "azure-native:dbforpostgresql:database",
		"azure-native:dbformysql:server", "azure-native:dbformysql:database",
		"azure-native:documentdb:databaseaccount", "azure-native:storage:storageaccount",
		"azure-native:storage:blobcontainer", "azure-native:storage:fileshare",
		"azure-native:keyvault:vault", "azure-native:keyvault:key", "azure-native:compute:disk",
		"azure-native:network:zone", "azure-native:network:privatezone", "azure-native:cache:redis",
		"azure-native:keyvault:secret", "azure-native:storage:table", "azure-native:storage:queue",
		"azure-native:documentdb:sqlresourcesqldatabase", "azure-native:documentdb:sqlresourcesqlcontainer",
		"azure-native:documentdb:mongodbresourcemongodbdatabase", "azure-native:documentdb:mongodbresourcemongodbcollection",
		"azure-native:documentdb:cassandraresourcecassandrakeyspace", "azure-native:documentdb:cassandraresourcecassandratable",
		"azure-native:documentdb:gremlinresourcegremlindatabase", "azure-native:documentdb:gremlinresourcegremlingraph",
		"azure-native:documentdb:tableresourcetable",
		"aws-native:rds:dbinstance", "aws-native:rds:dbcluster", "aws-native:dynamodb:table", "aws-native:dynamodb:globaltable",
		"aws-native:s3:bucket", "aws-native:kms:key", "aws-native:efs:filesystem",
	}
}

// Stateful reports whether a resource type holds data whose delete or
// replace loses it. Terraform and CloudFormation types match exactly; ARM
// types and Pulumi tokens match without regard to case, Pulumi tokens by
// family prefix. Database firewall rules, policies and similar satellites
// are not stateful.
func Stateful(typ string) bool {
	if typ == "" {
		return false
	}
	if statefulExact[typ] {
		return true
	}
	lt := strings.ToLower(typ)
	if strings.Contains(typ, "/") && !strings.Contains(typ, ":") && statefulExact[lt] {
		return true // ARM
	}
	if strings.Contains(typ, ":") && !strings.Contains(typ, "::") {
		for _, p := range statefulPrefix {
			if strings.HasPrefix(lt, p) {
				// "azure-native:sql:server" must not match "...:serverazureadadministrator".
				rest := lt[len(p):]
				if strings.HasSuffix(p, ":") || strings.HasSuffix(p, "/") || rest == "" {
					return true
				}
			}
		}
	}
	return false
}
