package aws

import (
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/iam"
	viper "github.com/openshift/osde2e/pkg/common/concurrentviper"
	"github.com/openshift/osde2e/pkg/common/config"
	"k8s.io/apimachinery/pkg/util/wait"
)

type ccsAwsSession struct {
	session *session.Session
	once    sync.Once
}

// CCSAWSSession is the global AWS session for interacting with AWS.
var CcsAwsSession ccsAwsSession

// AWS check for osdCCSAdmin credentials
func VerifyCCS() (string, error) {
	session, err := CcsAwsSession.getSession()
	if err != nil {
		return "", err
	}

	svc := iam.New(session)
	result, err := svc.GetUser(&iam.GetUserInput{})
	if err != nil {
		return "", err
	}

	if *result.User.UserName != "osdCcsAdmin" {
		log.Printf("The user %s is not osdCcsAdmin", *result.User.UserName)
	}

	return string(*result.User.UserName), nil
}

func (a *ccsAwsSession) getSession() (*session.Session, error) {
	var err error

	a.once.Do(func() {
		a.session, err = session.NewSession(aws.NewConfig().
			WithCredentials(credentials.NewStaticCredentials(viper.GetString("ocm.aws.accessKey"), viper.GetString("ocm.aws.secretKey"), "")).
			WithRegion(viper.GetString(config.CloudProvider.Region)))

		if err != nil {
			log.Printf("error initializing AWS session: %v", err)
		}
	})

	if a.session == nil {
		err = fmt.Errorf("unable to initialize AWS session")
	}

	return a.session, err
}

func (a *ccsAwsSession) GenerateCCSKeyPair() (string, string, error) {
	svc := iam.New(CcsAwsSession.session) //Reuses the session

	wait.PollImmediate(2*time.Minute, 90*time.Minute, func() (bool, error) {
		//Grabs existing keys
		keys, err := svc.ListAccessKeys(&iam.ListAccessKeysInput{
			UserName: aws.String("osdCcsAdmin"),
		})
		if err != nil {
			log.Printf("error listing keys: %v", err)
			return false, err
		}

		switch {
		case len(keys.AccessKeyMetadata) < 2:
			return true, nil
		case len(keys.AccessKeyMetadata) == 2:
			for _, key := range keys.AccessKeyMetadata {
				//If the create date is older than 5 minutes, delete the key
				//This should be enough time for OCM to finish with old CCS keys used to create a cluster.
				if key.CreateDate.Before(time.Now().Add(-5 * time.Minute)) {
					_, err := svc.DeleteAccessKey(&iam.DeleteAccessKeyInput{
						AccessKeyId: key.AccessKeyId,
						UserName:    aws.String("osdCcSAdmin"),
					})
					if err != nil {
						log.Printf("error deleting key: %v", err)
						return false, err
					}
				}

			}
		}
		return false, fmt.Errorf("unable to generate key pair")
	})

	ccsKeys, err := svc.CreateAccessKey(&iam.CreateAccessKeyInput{
		UserName: aws.String("osdCcSAdmin"),
	})
	if err != nil {
		return "", "", err
	} else {
		log.Printf("Created new key pair for osdCcsAdmin")
	}

	return *ccsKeys.AccessKey.AccessKeyId, *ccsKeys.AccessKey.SecretAccessKey, nil
}
