use actix_web::{web, App, HttpServer, HttpResponse};
use serde::{Deserialize, Serialize};

#[derive(Debug, Serialize, Deserialize, Clone)]
struct MatchPrediction {
    home_team: String,
    away_team: String,
    home_goals: u8,
    away_goals: u8,
    username: String,
    timestamp: String,
}

#[derive(Serialize)]
struct ApiResponse {
    status: String,
    message: String,
}

async fn receive_prediction(
    prediction: web::Json<MatchPrediction>,
    go_client_url: web::Data<String>,
) -> HttpResponse {
    let client = reqwest::Client::new();
    
    // Reenviar al Go Service
    match client
        .post(format!("{}/predict", go_client_url.get_ref()))
        .json(&prediction.0)
        .send()
        .await
    {
        Ok(_) => HttpResponse::Ok().json(ApiResponse {
            status: "ok".to_string(),
            message: "Prediction received".to_string(),
        }),
        Err(e) => HttpResponse::InternalServerError().json(ApiResponse {
            status: "error".to_string(),
            message: e.to_string(),
        }),
    }
}

async fn health() -> HttpResponse {
    HttpResponse::Ok().json(ApiResponse {
        status: "ok".to_string(),
        message: "Rust API is running".to_string(),
    })
}

#[actix_web::main]
async fn main() -> std::io::Result<()> {
    let go_client_url = std::env::var("GO_CLIENT_URL")
        .unwrap_or_else(|_| "http://go-client:8080".to_string());

    println!("Starting Rust API on port 8080...");
    
    HttpServer::new(move || {
        App::new()
            .app_data(web::Data::new(go_client_url.clone()))
            .route("/health", web::get().to(health))
            .route("/predict", web::post().to(receive_prediction))
    })
    .bind("0.0.0.0:8080")?
    .run()
    .await
}
