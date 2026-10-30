@Library('avio.library@main') _

def source
def releasePlan

pipeline {
    agent none
    options {
        disableConcurrentBuilds()
        skipDefaultCheckout()
        preserveStashes(buildCount: 5)
        timeout(time: 60, unit: 'MINUTES')
        buildDiscarder(logRotator(numToKeepStr: '20', artifactNumToKeepStr: '5'))
        timestamps()
    }
    triggers { githubPush(); pollSCM('H/5 * * * *') }
    stages {
        stage('Resolve source') {
            agent { label 'standalone-linux-build && amd64' }
            steps {
                script {
                    source = checkoutSource(clean: true)
                    releasePlan = cliReleasePlan(source: source, versionFile: 'AVIO_VERSION',
                        branch: 'master', versionSuffix: '-avio')
                }
            }
        }
        stage('Build distributions') {
            parallel {
                stage('Linux x64') {
                    agent { label 'standalone-linux-build && amd64' }
                    steps { script { buildDistribution('linux', 'linux-x64', source, releasePlan) } }
                }
                stage('macOS ARM64') {
                    agent { label 'standalone-macos-build && arm64' }
                    steps { script { buildDistribution('darwin', 'darwin-arm64', source, releasePlan) } }
                }
                stage('Windows x64') {
                    agent { label 'standalone-windows-build && amd64' }
                    steps { script { buildDistribution('windows', 'windows-x64', source, releasePlan) } }
                }
            }
        }
        stage('Publish channel and finalize release') {
            when {
                beforeAgent true
                expression { releasePlan.publish }
            }
            agent { label 'standalone-linux-build && amd64' }
            steps {
                script {
                    checkoutSource(clean: true, commit: source.commit)
                    def expected = ['linux-x64', 'darwin-arm64', 'windows-x64'].collect {
                        "bkt-native-${it}-${releasePlan.version}.${it == 'windows-x64' ? 'zip' : 'tar.gz'}".toString()
                    }
                    def artifacts = collectArtifacts(plan: releasePlan, targets: ['linux-x64', 'darwin-arm64', 'windows-x64'], expected: expected)
                    publishCliRelease(project: 'bkt', plan: releasePlan, artifacts: artifacts, expected: expected)
                }
            }
        }
    }
}

def buildDistribution(String platform, String target, Map source, Map plan) {
    checkoutSource(clean: true, commit: source.commit)
    withProjectTools(tools: ['go'], defaults: [go: '1.26.8']) { tools ->
        if (platform == 'windows') {
            withMsys2 {
                bat 'bash scripts/ci-avio.sh windows'
            }
        } else sh "bash scripts/ci-avio.sh ${platform}"
    }
    recordArtifacts(plan: plan, target: target,
        includes: "dist/avio/bkt-native-${target}-${plan.version}.${platform == 'windows' ? 'zip' : 'tar.gz'}")
}
