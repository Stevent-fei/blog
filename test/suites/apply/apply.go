package apply

import (
	"blog/test/testhelper"
	"blog/test/testhelper/settings"
	"bytes"
	"fmt"
	"github.com/alibaba/sealer/pkg/infra"
	v1 "github.com/alibaba/sealer/types/api/v1"
	"github.com/alibaba/sealer/utils"
	"github.com/alibaba/sealer/utils/ssh"
	"github.com/onsi/gomega"
	"gopkg.in/yaml.v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

func LoadClusterFileFromDisk(clusterFilePath string) *v1.Cluster {
	clusters, err := utils.DecodeCluster(clusterFilePath)
	testhelper.CheckErr(err)
	testhelper.CheckNotNil(clusters[0])
	return &clusters[0]
}

func getFixtures() string {
	pwd := settings.DefaultTestEnvDir
	return filepath.Join(pwd, "suites", "apply", "fixtures")
}

func GetRawClusterFilePath() string {
	fixtures := getFixtures()
	return filepath.Join(fixtures, "cluster_file_for_test.yaml")
}

func CreateAliCloudInfraAndSave(cluster *v1.Cluster, clusterFile string) *v1.Cluster {
	CreateAliCloudInfra(cluster)
	//save used cluster file
	cluster.Spec.Provider = settings.BAREMETAL
	MarshalClusterToFile(clusterFile, cluster)
	cluster.Spec.Provider = settings.AliCloud
	return cluster
}

func CreateAliCloudInfra(cluster *v1.Cluster) {
	cluster.DeletionTimestamp = nil
	infraManager, err := infra.NewDefaultProvider(cluster)
	testhelper.CheckErr(err)
	err = infraManager.Apply()
	testhelper.CheckErr(err)
}

func MarshalClusterToFile(ClusterFile string, cluster *v1.Cluster) {
	err := testhelper.MarshalYamlToFile(ClusterFile, &cluster)
	testhelper.CheckErr(err)
	testhelper.CheckNotNil(cluster)
}

func CleanUpAliCloudInfra(cluster *v1.Cluster) {
	if cluster == nil {
		return
	}
	if cluster.Spec.Provider != settings.AliCloud {
		cluster.Spec.Provider = settings.AliCloud
	}
	t := metav1.Now()
	cluster.DeletionTimestamp = &t
	infraManager, err := infra.NewDefaultProvider(cluster)
	testhelper.CheckErr(err)
	err = infraManager.Apply()
	testhelper.CheckErr(err)
}

func SendAndRunCluster(sshClient *testhelper.SSHClient, clusterFile string, joinMasters, joinNodes, passwd string) {
	SendAndRemoteExecCluster(sshClient, clusterFile, SealerRunCmd(joinMasters, joinNodes, passwd, ""))
}

func SendAndRemoteExecCluster(sshClient *testhelper.SSHClient, clusterFile string, remoteCmd string) {
	// send tmp cluster file to remote server and run apply cmd
	gomega.Eventually(func() bool {
		err := sshClient.SSH.Copy(sshClient.RemoteHostIP, clusterFile, clusterFile)
		return err == nil
	}, settings.MaxWaiteTime).Should(gomega.BeTrue())
	err := sshClient.SSH.CmdAsync(sshClient.RemoteHostIP, remoteCmd)
	testhelper.CheckErr(err)
}

func SealerRunCmd(masters, nodes, passwd string, provider string) string {
	if masters != "" {
		masters = fmt.Sprintf("-m %s", masters)
	}
	if nodes != "" {
		nodes = fmt.Sprintf("-n %s", nodes)
	}
	if passwd != "" {
		passwd = fmt.Sprintf("-p %s", passwd)
	}
	if provider != "" {
		provider = fmt.Sprintf("--provider %s", provider)
	}
	return fmt.Sprintf("%s run %s -e %s %s %s %s %s -d", settings.DefaultSealerBin, settings.TestImageName, settings.CustomCalicoEnv , masters, nodes, passwd, provider)
}

// CheckNodeNumWithSSH check node mum of remote cluster;for bare metal apply
func CheckNodeNumWithSSH(sshClient *testhelper.SSHClient, expectNum int) {
	if sshClient == nil {
		return
	}
	cmd := "kubectl get nodes | wc -l"
	result, err := sshClient.SSH.CmdToString(sshClient.RemoteHostIP, cmd, "")
	testhelper.CheckErr(err)
	num, err := strconv.Atoi(strings.ReplaceAll(result, "\n", ""))
	testhelper.CheckErr(err)
	testhelper.CheckEqual(num, expectNum+1)
}

func GenerateClusterfile(clusterfile string) {
	filepath := GetRawClusterFilePath()
	cluster := LoadClusterFileFromDisk(clusterfile)
	cluster.Spec.Env = []string{"Network=calico"}
	data, err := yaml.Marshal(cluster)
	testhelper.CheckErr(err)
	appendData := [][]byte{data} //二维数组，key，value
	plugins := LoadPluginFromDisk(filepath)
	configs := LoadConfigFromDisk(filepath)
	for _, plugin := range plugins {
		if plugin.Spec.Type == "LABEL" {
			pluginData := "\n"
			for _, ip := range cluster.Spec.Masters.IPList {
				pluginData += fmt.Sprintf("%s sealer-test=true \n", ip)
			}
			plugin.Spec.Data = pluginData
		}
		if plugin.Spec.Type == "HOSTNAME" {
			pluginData := "\n"
			for i, ip := range cluster.Spec.Masters.IPList {
				pluginData += fmt.Sprintf("%s master-%s\n", ip, strconv.Itoa(i))
			}
			for i, ip := range cluster.Spec.Nodes.IPList {
				pluginData += fmt.Sprintf("%s master-%s\n", ip, strconv.Itoa(i))
			}
			plugin.Spec.Data = pluginData
		}
		data, err := yaml.Marshal(plugin)
		testhelper.CheckErr(err)
		appendData = append(appendData, []byte("---\n"), data)
	}
	for _, config := range configs{
		data, err := yaml.Marshal(config)
		testhelper.CheckErr(err)
		appendData = append(appendData, []byte("---\n"), data)
	}
	err = utils.WriteFile(clusterfile, bytes.Join(appendData, []byte("")))
	testhelper.CheckErr(err)
}

func LoadPluginFromDisk(clusterfilePath string)[]v1.Plugin  {
	plugins, err := utils.DecodePlugins(clusterfilePath)
	testhelper.CheckErr(err)
	testhelper.CheckNotNil(plugins)
	return plugins
}

func LoadConfigFromDisk(Clusterfilepath string)[]v1.Config  {
	configs, err := utils.DecodeConfigs(Clusterfilepath)
	testhelper.CheckErr(err)
	testhelper.CheckNotNil(configs)
	return configs
}

func SendAndApplyCluster(sshClient *testhelper.SSHClient, clusterFile string) {
	SendAndRemoteExecCluster(sshClient, clusterFile, SealerApplyCmd(clusterFile))
}

func SealerApplyCmd(clusterFile string) string {
	return fmt.Sprintf("%s apply -f %s --force -d", settings.DefaultSealerBin, clusterFile)
}

func WaitAllNodeRunningBySSH(s ssh.Interface, masterIp string) {
	time.Sleep(30 * time.Second)
	err := utils.Retry(10,5 * time.Second, func() error {
		result, err := s.CmdToString(masterIp, "kubectl get node", "")
		if err != nil{
			return err
		}
		if strings.Contains(result, "NotReady") {
			return fmt.Errorf("node not ready: \n %s", result)
		}
		return nil
	})
	testhelper.CheckErr(err)
}

func SealerDeleteCmd(clusterFile string) string {
	return fmt.Sprintf("%s delete -f %s --force -d", settings.DefaultSealerBin,clusterFile)
}