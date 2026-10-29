pipeline {
    agent none
    options {
        disableConcurrentBuilds()
        skipDefaultCheckout()
        timeout(time: 60, unit: 'MINUTES')
        buildDiscarder(logRotator(numToKeepStr: '20', artifactNumToKeepStr: '5'))
        timestamps()
    }
    triggers { pollSCM('H/5 * * * *') }
    stages {
        stage('Resolve source') {
            agent { label 'standalone-linux-build && amd64' }
            steps {
                deleteDir()
                checkout scm
                script {
                    env.SOURCE_COMMIT = sh(script: 'git rev-parse HEAD', returnStdout: true).trim()
                    env.BKT_RELEASE_BUILD = "jenkins-${env.BUILD_NUMBER}-${env.SOURCE_COMMIT}"
                }
            }
        }
        stage('Build distributions') {
            parallel {
                stage('Linux x64') {
                    agent { label 'standalone-linux-build && amd64' }
                    steps { script { buildDistribution('linux') } }
                }
                stage('macOS ARM64') {
                    agent { label 'standalone-macos-build && arm64' }
                    steps { script { buildDistribution('darwin') } }
                }
            }
        }
        stage('Publish snapshot channel') {
            agent { label 'standalone-linux-build && amd64' }
            steps {
                script {
                    checkoutSource()
                    unstash 'linux-distribution'
                    unstash 'darwin-distribution'
                    // A feature-branch validation job never receives publishing credentials.
                    def master = sh(script: 'git rev-parse refs/remotes/origin/master', returnStdout: true).trim()
                    if (env.SOURCE_COMMIT == master && !env.CHANGE_ID && !env.TAG_NAME) {
                        withCredentials([usernamePassword(credentialsId: 'artifactory-devops',
                                usernameVariable: 'JFROG_USERNAME', passwordVariable: 'JFROG_ACCESS_TOKEN')]) {
                            sh 'bash scripts/ci-avio.sh publish'
                        }
                    } else {
                        echo 'Validation only: publication requires the current master commit.'
                    }
                }
            }
        }
    }
}

def checkoutSource() {
    deleteDir()
    checkout scm
    sh 'git checkout --detach "$SOURCE_COMMIT"'
}

def buildDistribution(String platform) {
    checkoutSource()
    sh "bash scripts/ci-avio.sh ${platform}"
    archiveArtifacts artifacts: 'dist/avio/*', fingerprint: true
    stash name: "${platform}-distribution", includes: 'dist/avio/*'
}
