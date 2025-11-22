# Traefik Request Logger Plugin

Traefik plugin to log the requests to the access log file.

## Usage

To use the plugin, add the following to your Traefik static configuration:
```yaml
experimental:
  plugins:
    request_logger:
      moduleName: github.com/lakshanwd/traefikrequestlogger
      version: v1.0.0
```

Also you need to add the plugin to the dynamic configuration:
```yaml
http:
  routers:
    web:
      ...
      middlewares:
        - logger_middleware

  middlewares:
    logger_middleware:
      plugin:
        request_logger:
          path: /var/log/traefik/access.log
          interval: 3
```

## Configuration

| Configuration | Description | Default |
|---------------|-------------|---------|
| path | The path to the access log file. | /dev/stdout |
| interval | The interval to flush the log file. | 0s |