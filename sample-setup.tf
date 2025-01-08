terraform {
  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 5.0" # Or your preferred version
    }
  }
}

provider "aws" {
  region = "us-west-2" # Replace with your desired region
}

# VPC
resource "aws_vpc" "ing_main" {
  cidr_block = "10.0.0.0/16"
  enable_dns_hostnames = true
  tags = {
    Name = "ing-main-vpc"
  }
}

resource "aws_subnet" "ing_public" {
  count             = 2
  vpc_id            = aws_vpc.ing_main.id
  cidr_block        = cidrsubnet(aws_vpc.ing_main.cidr_block, 8, count.index)
  availability_zone = data.aws_availability_zones.available.names[count.index]
  map_public_ip_on_launch = true
  tags = {
    Name = "ing-public-subnet-${count.index + 1}"
  }
}

data "aws_availability_zones" "available" {
  state = "available"
}

# Internet Gateway
resource "aws_internet_gateway" "ing_gw" {
  vpc_id = aws_vpc.ing_main.id

  tags = {
    Name = "ing-main-igw"
  }
}

resource "aws_route_table" "ing_public" {
  vpc_id = aws_vpc.ing_main.id
}

resource "aws_route" "ing_public" {
  route_table_id         = aws_route_table.ing_public.id
  destination_cidr_block = "0.0.0.0/0"
  gateway_id             = aws_internet_gateway.ing_gw.id
}

resource "aws_route_table_association" "ing_public" {
  count          = 2
  subnet_id      = aws_subnet.ing_public[count.index].id
  route_table_id = aws_route_table.ing_public.id
}

# ECS Cluster
resource "aws_ecs_cluster" "ing_main" {
  name = "ing-main-cluster"
}

# ECS Task Definition
resource "aws_ecs_task_definition" "ing_app" {
  family                   = "ing-app-task"
  execution_role_arn       = aws_iam_role.ing_ecs_task_execution_role.arn
  task_role_arn            = aws_iam_role.ing_ecs_task_role.arn
  container_definitions = jsonencode([
    {
      name      = "ing-app-container"
      image     = "905820938707.dkr.ecr.us-west-2.amazonaws.com/shailxeta/basic-ws-server:latest"
      portMappings = [
        {
          containerPort = 8080 # Container listens on 8080
          hostPort      = 8080 # Host port (ephemeral with Fargate)
        }
      ],
        logConfiguration = {
        logDriver = "awslogs",
        options = {
          "awslogs-group"         = aws_cloudwatch_log_group.ing_ecs_logs.name,
          "awslogs-region"        = "us-east-1",
          "awslogs-stream-prefix" = "ecs"
        }
      }
    }
  ])
  requires_compatibilities = ["FARGATE"]
  network_mode             = "awsvpc"
  cpu                      = 256
  memory                   = 512
}

# ECS Service
resource "aws_ecs_service" "ing_app" {
  name            = "ing-app-service"
  cluster         = aws_ecs_cluster.ing_main.id
  task_definition = aws_ecs_task_definition.ing_app.arn
  desired_count   = 1 # Number of tasks
  launch_type     = "FARGATE"
  network_configuration {
    subnets          = [aws_subnet.ing_public[0].id, aws_subnet.ing_public[1].id]
    security_groups = [aws_security_group.ing_allow_all.id]
  }
  load_balancer {
    target_group_arn = aws_lb_target_group.ing_app.arn
    container_name   = "ing-app-container"
    container_port   = 8080 # Important: Match container port
  }
  depends_on = [aws_lb_listener.ing_front]
}

# ALB
resource "aws_lb" "ing_app" {
  name               = "ing-app-alb"
  internal           = false
  load_balancer_type = "application"
    subnets          = [aws_subnet.ing_public[0].id, aws_subnet.ing_public[1].id]
  security_groups = [aws_security_group.ing_allow_all.id]
}

resource "aws_lb_target_group" "ing_app" {
  name     = "ing-app-tg"
  port     = 8080 # Target group sends traffic to port 8080
  protocol = "HTTP"
  vpc_id   = aws_vpc.ing_main.id
    health_check {
    path = "/health"
    protocol = "HTTP"
    matcher = "200"
    interval = 10
    timeout = 5
    healthy_threshold = 3
    unhealthy_threshold = 3
  }
}

resource "aws_lb_listener" "ing_front" {
  load_balancer_arn = aws_lb.ing_app.arn
  port              = 80
  protocol          = "HTTP"

  default_action {
    type             = "forward"
    target_group_arn = aws_lb_target_group.ing_app.arn
  }
}

# Security Group (Allow All - FOR DEMO ONLY! In production, restrict this significantly)
resource "aws_security_group" "ing_allow_all" {
  name        = "ing_allow_all"
  description = "Allow all inbound traffic (INSECURE - FOR DEMO ONLY)"
  vpc_id      = aws_vpc.ing_main.id

  ingress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }
    egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }
}

# IAM Roles (Essential for Fargate)
resource "aws_iam_role" "ing_ecs_task_execution_role" {
  name = "ing-ecs-task-execution-role"

  assume_role_policy = jsonencode({
    Version = "2012-10-17",
    Statement = [
      {
        Action = "sts:AssumeRole",
        Effect = "Allow",
        Principal = {
          Service = "ecs-tasks.amazonaws.com"
        }
      }
    ]
  })
}

resource "aws_iam_policy_attachment" "ing_ecs_task_execution_policy_attachment" {
  name       = "ing_ecs_task_execution_policy_attachment"  # Add a descriptive name
  policy_arn = "arn:aws:iam::aws:policy/service-role/AmazonECSTaskExecutionRolePolicy"
  roles      = aws_iam_role.ing_ecs_task_execution_role.name
}

resource "aws_iam_role" "ing_ecs_task_role" {
  name = "ing-ecs-task-role"

  assume_role_policy = jsonencode({
    Version = "2012-10-17",
    Statement = [
      {
        Action = "sts:AssumeRole",
        Effect = "Allow",
        Principal = {
          Service = "ecs-tasks.amazonaws.com"
        }
      }
    ]
  })
}

# Cloudwatch Log Group
resource "aws_cloudwatch_log_group" "ing_ecs_logs" {
  name = "/ecs/ing-app-logs"
}
