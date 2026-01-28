docker_build('circa10a/dead-mans-switch', '.', dockerfile='./Dockerfile')
k8s_yaml(listdir('./deploy/k8s'))
k8s_resource('dead-mans-switch', port_forwards=8080)