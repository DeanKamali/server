#!/bin/bash

# Build and push all Docker images to stackblaze repository

set -e

# Configuration
REGISTRY="${REGISTRY:-docker.io}"
REGISTRY_USER="${REGISTRY_USER:-stackblaze}"
IMAGE_TAG="${IMAGE_TAG:-latest}"

# Colors
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

echo -e "${BLUE}🐳 Building and pushing all images to stackblaze${NC}"
echo ""

# Function to build and push an image
build_and_push() {
    local component=$1
    local dockerfile=$2
    local image_name="$REGISTRY/$REGISTRY_USER/$component:$IMAGE_TAG"
    
    echo -e "${YELLOW}📦 Building $component...${NC}"
    
    # Build the component binary if needed
    if [ ! -f "$component/$component" ]; then
        echo "   Building $component binary..."
        cd "$component"
        if [ -f "build.sh" ]; then
            chmod +x build.sh
            ./build.sh
        else
            echo "   Error: build.sh not found in $component/"
            return 1
        fi
        cd ..
    fi
    
    # Build Docker image
    echo "   Building Docker image: $image_name"
    docker build -f "$dockerfile" -t "$image_name" "$component/"
    
    # Tag as latest if not already
    if [ "$IMAGE_TAG" != "latest" ]; then
        docker tag "$image_name" "$REGISTRY/$REGISTRY_USER/$component:latest"
    fi
    
    # Push image
    echo "   Pushing to registry..."
    docker push "$image_name"
    
    if [ "$IMAGE_TAG" != "latest" ]; then
        docker push "$REGISTRY/$REGISTRY_USER/$component:latest"
    fi
    
    echo -e "${GREEN}   ✅ $component pushed: $image_name${NC}"
    echo ""
}

# Check Docker login
if ! docker info | grep -q "Username"; then
    echo -e "${YELLOW}⚠️  Not logged into Docker registry${NC}"
    echo "   Run: docker login $REGISTRY"
    read -p "   Do you want to login now? (y/n) " -n 1 -r
    echo
    if [[ $REPLY =~ ^[Yy]$ ]]; then
        docker login "$REGISTRY"
    else
        echo "   Skipping push. You can push later."
        exit 0
    fi
fi

# Build and push all components
build_and_push "page-server" "page-server/Dockerfile"
build_and_push "safekeeper" "safekeeper/Dockerfile"
build_and_push "control-plane" "control-plane/Dockerfile"

echo -e "${GREEN}=========================================="
echo "✅ All images built and pushed!"
echo "==========================================${NC}"
echo ""
echo "Images:"
echo "  - $REGISTRY/$REGISTRY_USER/page-server:$IMAGE_TAG"
echo "  - $REGISTRY/$REGISTRY_USER/safekeeper:$IMAGE_TAG"
echo "  - $REGISTRY/$REGISTRY_USER/control-plane:$IMAGE_TAG"
echo ""
echo "To use in Kubernetes:"
echo "  kubectl run page-server --image=$REGISTRY/$REGISTRY_USER/page-server:$IMAGE_TAG"
echo "  kubectl run safekeeper --image=$REGISTRY/$REGISTRY_USER/safekeeper:$IMAGE_TAG"
echo "  kubectl run control-plane --image=$REGISTRY/$REGISTRY_USER/control-plane:$IMAGE_TAG"

