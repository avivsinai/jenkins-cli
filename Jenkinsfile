pipeline {
  agent any

  options {
    skipDefaultCheckout()
  }

  environment {
    GOFLAGS = "-p=1"
    GODEBUG = "asyncpreemptoff=1"
    GOMAXPROCS = "1"
    JK_E2E_DISABLE = "1"
  }

  stages {
    stage('Checkout') {
      steps {
        checkout scm
      }
    }
    stage('Build') {
      steps {
        sh 'go env GOHOSTARCH GOTOOLDIR'
        sh 'make build'
      }
    }
    stage('Unit Tests') {
      steps {
        sh 'make test'
      }
      post {
        always {
          junit allowEmptyResults: true, testResults: '**/junit*.xml'
        }
      }
    }
  }

  post {
    always {
      archiveArtifacts artifacts: 'bin/**', allowEmptyArchive: true
    }
  }
}
