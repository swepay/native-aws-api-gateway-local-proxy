"""
Lambda Mock Handler - Para testes do API Gateway Local Proxy
Ecoa o evento recebido e demonstra o formato correto de resposta
"""

import json
from datetime import datetime


def handler(event, context):
    """
    Handler de exemplo que ecoa o evento recebido.
    Demonstra como uma Lambda real deve responder.
    """
    
    # Log do evento recebido
    print(f"📥 Received event: {json.dumps(event, indent=2)}")
    
    # Extrair informações do evento
    version = event.get("version", "unknown")
    method = event.get("requestContext", {}).get("http", {}).get("method", "unknown")
    path = event.get("rawPath", "/")
    path_params = event.get("pathParameters", {})
    body = event.get("body", "")
    
    # Construir resposta de exemplo
    response_body = {
        "message": "Lambda executada com sucesso via API Gateway Local Proxy!",
        "timestamp": datetime.utcnow().isoformat() + "Z",
        "receivedEvent": {
            "version": version,
            "method": method,
            "path": path,
            "pathParameters": path_params,
            "hasBody": bool(body),
            "bodyLength": len(body) if body else 0
        }
    }
    
    # Retornar resposta no formato API Gateway v2
    return {
        "statusCode": 200,
        "headers": {
            "Content-Type": "application/json",
            "X-Custom-Header": "lambda-mock",
            "X-Request-Id": event.get("requestContext", {}).get("requestId", "unknown")
        },
        "body": json.dumps(response_body, indent=2)
    }
