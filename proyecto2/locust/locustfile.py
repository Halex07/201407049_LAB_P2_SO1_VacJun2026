from locust import HttpUser, task, between
import random
from datetime import datetime, timezone

TEAMS = ["GTM", "MEX", "BRA", "ARG", "ESP"]

class QuinielaUser(HttpUser):
    wait_time = between(0.5, 2)

    @task
    def send_prediction(self):
        home_team = random.choice(TEAMS)
        away_team = random.choice([t for t in TEAMS if t != home_team])

        payload = {
            "home_team": home_team,
            "away_team": away_team,
            "home_goals": random.randint(0, 5),
            "away_goals": random.randint(0, 5),
            "username": f"user_{random.randint(1, 1000)}",
            "timestamp": datetime.now(timezone.utc).isoformat()
        }

        with self.client.post(
            "/grpc-201407049",
            json=payload,
            headers={"Content-Type": "application/json"},
            catch_response=True
        ) as response:
            if response.status_code == 200:
                response.success()
            else:
                response.failure(f"Error: {response.status_code}")
