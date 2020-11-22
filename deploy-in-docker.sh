#!/usr/bin/env sh

black="\033[0;30m"
darkGray="\033[1;30m"
red="\033[0;31m"
lightRed="\033[1;31m"
green="\033[0;32m"
lightGreen="\033[1;32m"
orange="\033[0;33m"
yellow="\033[1;33m"
blue="\033[0;34m"
lightBlue="\033[1;34m"
purple="\033[0;35m"
lightPurple="\033[1;35m"
cyan="\033[0;36m"
lightCyan="\033[1;36m"
lightGray="\033[0;37m"
white="\033[1;37m"
nc="\033[0m"

descColor=$darkGray
promptColor=$green
defValColor=$white
required="${orange}REQUIRED${nc}"

ApplicationDir=`realpath "$(dirname $0)"`
ApplicationName=`echo $(basename "$ApplicationDir") | sed -e "s/_/-/g"`
DefaultNamespace="devops-webhooks"

args="--command deploy --folder deployment-scripts"

echo "I will ask you a couple of question to configure the deployment."
echo "Almost all configurations have default value and you may accept it with just pressing ENTER"
echo ""

echo "${descColor}Indicate whether server should run in insecure mode(this is just for debug)${nc}"
echo -n "${promptColor}Insecure${nc}(${defValColor}false${nc}): "
read Insecure
case $Insecure in
    yes|1|true|ok)
        args="${args} --insecure"
        echo "${orange}Warning: Insecure mode enabled${nc}"
        ;;
    *)
        echo "${descColor}Certificate file that should used by the webhook for secure connection${nc}"
        echo -n "${promptColor}Certificate${nc}(${defValColor}An auto generated certificate${nc}): "
        read Certificate
        if ! [ -z "$Certificate" ]; then
            args="$args --cert '$Certificate'"
            echo "${descColor}Private key file that should used together with certificate${nc}"
            while [ -z "$PrivateKey" ]; do
                echo -n "${promptColor}PrivateKey${nc}(${required}): "
                read PrivateKey
                if [ -z "$PrivateKey" ]; then
                    echo "${red}When you specify the certificate then its private key is required${nc}"
                fi
            done
            args="$args --key ${PrivateKey}"

            echo "${descColor}If this certificate is signed by a custom CA, then you must provide the${nc}"
            echo "${descColor}file that contains certificate of that CA here${nc}"
            echo -n "${promptColor}CAFile$nc}(${defValColor}EMPTY${nc}): "
            read -n CAFile
            if ! [ -z "$CAFile" ]; then
                args="$args --ca '${CAFile}'"
            fi
        fi

        echo "${descColor}Name of the secret that contains TLS secrets of the service${nc}"
        echo -n "${promptColor}SecretName${nc}(${defValColor}${ApplicationName}-tls${nc}): "
        read SecretName
        if ! [ -z "$SecretName" ]; then
            args="$args --secret-name '${SecretName}'"
        fi
        ;;
esac

echo "${descColor}Name of the image that will be created from this application.${nc}"
echo -n "${promptColor}ImageName${nc}(${defValColor}${ApplicationName}${nc}): "
read ImageName
if ! [ -z "$ImageName" ]; then
    args="$args --image '$ImageName'"
fi

echo "${descColor}Tag of the created image${nc}"
echo -n "${promptColor}ImageTag${nc}(${defValColor}latest${nc}): "
read ImageTag
if ! [ -z "$ImageTag" ]; then
    args="$args --tag '$ImageTag'"
fi

echo "${descColor}Registry that image should pushed to it. You must already be logged into this repository${nc}"
echo -n "${promptColor}ImageRegistry${nc}(${defValColor}system default registry${nc}): "
read ImageRegistry
if ! [ -z "$ImageRegistry" ]; then
    args="$args --registry '$ImageRegistry'"
fi

echo "${descColor}If you want to run the image as a non-root user, you must specify its user ID here${nc}"
echo -n "${promptColor}RunAsUser${nc}(${defValColor}1234${nc}): "
read RunAsUser
if ! [ -z "$RunAsUser" ]; then
    args="$args --runas '$RunAsUser'"
fi

echo "${descColor}Namespace that webhook should deployed to it.${nc}"
echo -n "${promptColor}WebhookNamespace${nc}(${defValColor}${DefaultNamespace}${nc}): "
read Namespace
if ! [ -z "$Namespace" ]; then
    args="$args --namespace '$Namespace'"
fi

echo "${descColor}Name of the service of this webhook${nc}"
echo -n "${promptColor}ServiceName${nc}(${defValColor}${ApplicationName}${nc}): "
read ServiceName
if ! [ -z "$ServiceName" ]; then
    args="$args --service-name '${ServiceName}'"
fi

echo "${descColor}If building go application require a proxy to grab packages, then you must${nc}"
echo "${descColor}provide it here${nc}"
echo -n "${promptColor}BuildProxy${nc}(${defValColor}no proxy required${nc}): "
read BuildProxy
if ! [ -z "$BuildProxy" ]; then
    args="$args --proxy '${BuildProxy}'"
fi

echo ""
echo "Now we build the application in a docker image and create deployment scripts with specified parameters"

exec_command="cd /$ApplicationName && go build -o $ApplicationName && /$ApplicationName/$ApplicationName $args"
if ! [ -z "$BuildProxy" ]; then
    exec_command="export HTTP_PROXY='$BuildProxy'; export HTTPS_PROXY='$BuildProxy'; $exec_command"
fi

rm -rf "$ApplicationDir/$ApplicationName"   # make sure that executive does not exists
docker run -it --rm -v "$ApplicationDir:/$ApplicationName" golang:alpine3.12 /bin/sh -c "$exec_command"
rm -rf "$ApplicationDir/$ApplicationName"

echo "Now you may run ${white}deploy.sh${nc} to deploy the application"
