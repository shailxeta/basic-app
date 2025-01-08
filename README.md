Basic App

- basic-ws-server : websocket server with basic functionality
- basic-ws-client : websocket cloent with basic functionality


# aws login to docker 
aws ecr get-login-password --region us-west-2 --profile=sso-playground | docker login --username AWS --password-stdin 905820938707.dkr.ecr.us-west-2.amazonaws.com

docker tag shailxeta/basic-ws-server:latest 905820938707.dkr.ecr.us-west-2.amazonaws.com/shailxeta/basic-ws-server:latest
docker tag shailxeta/basic-ws-server:latest 905820938707.dkr.ecr.us-west-2.amazonaws.com/shailxeta/basic-ws-server:v1
docker push 905820938707.dkr.ecr.us-west-2.amazonaws.com/shailxeta/basic-ws-server:latest
docker push 905820938707.dkr.ecr.us-west-2.amazonaws.com/shailxeta/basic-ws-server:v1


docker tag shailxeta/basic-ws-client:latest 905820938707.dkr.ecr.us-west-2.amazonaws.com/shailxeta/basic-ws-client:latest
docker tag shailxeta/basic-ws-client:latest 905820938707.dkr.ecr.us-west-2.amazonaws.com/shailxeta/basic-ws-client:v1
docker push 905820938707.dkr.ecr.us-west-2.amazonaws.com/shailxeta/basic-ws-client:latest
docker push 905820938707.dkr.ecr.us-west-2.amazonaws.com/shailxeta/basic-ws-client:v1
