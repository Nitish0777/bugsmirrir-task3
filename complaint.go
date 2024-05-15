package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"sync"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

type User struct {
	ID         primitive.ObjectID   `bson:"_id,omitempty" json:"id"`
	SecretCode string               `json:"secretCode"`
	Name       string               `json:"name"`
	Email      string               `json:"email"`
	Complaints []primitive.ObjectID `json:"complaints"`
}

type Complaint struct {
	ID       primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	Title    string             `json:"title"`
	Summary  string             `json:"summary"`
	Rating   int                `json:"rating"`
	Resolved bool               `json:"resolved"`
	UserID   primitive.ObjectID `json:"userId"`
}

var client *mongo.Client
var db *mongo.Database
var mu sync.Mutex

func initDB() {
	var err error
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	clientOptions := options.Client().ApplyURI("mongodb://localhost:27017/complain")
	client, err = mongo.Connect(ctx, clientOptions)
	if err != nil {
		log.Fatal(err)
	}

	db = client.Database("complaintsPortal")
}

func generateSecretCode() string {
	return fmt.Sprintf("%06d", rand.Intn(1000000))
}

func loginHandler(w http.ResponseWriter, r *http.Request) {
	secretCode := r.URL.Query().Get("secretCode")
	var user User

	err := db.Collection("users").FindOne(context.TODO(), bson.M{"secretcode": secretCode}).Decode(&user)
	if err != nil {
		http.Error(w, "User not found", http.StatusNotFound)
		return
	}

	json.NewEncoder(w).Encode(user)
}

func registerHandler(w http.ResponseWriter, r *http.Request) {
	var user User
	if err := json.NewDecoder(r.Body).Decode(&user); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	user.ID = primitive.NewObjectID()
	user.SecretCode = generateSecretCode()
	user.Complaints = []primitive.ObjectID{}

	_, err := db.Collection("users").InsertOne(context.TODO(), user)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	json.NewEncoder(w).Encode(user)
}

func submitComplaintHandler(w http.ResponseWriter, r *http.Request) {
	var complaint Complaint
	if err := json.NewDecoder(r.Body).Decode(&complaint); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	complaint.ID = primitive.NewObjectID()
	complaint.Resolved = false

	_, err := db.Collection("complaints").InsertOne(context.TODO(), complaint)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	var user User
	err = db.Collection("users").FindOne(context.TODO(), bson.M{"_id": complaint.UserID}).Decode(&user)
	if err == nil {
		user.Complaints = append(user.Complaints, complaint.ID)
		_, err = db.Collection("users").UpdateOne(context.TODO(), bson.M{"_id": user.ID}, bson.M{"$set": bson.M{"complaints": user.Complaints}})
	}

	json.NewEncoder(w).Encode(complaint)
}

func getAllComplaintsForUserHandler(w http.ResponseWriter, r *http.Request) {
	secretCode := r.URL.Query().Get("secretCode")

	var user User
	err := db.Collection("users").FindOne(context.TODO(), bson.M{"secretcode": secretCode}).Decode(&user)
	if err != nil {
		http.Error(w, "User not found", http.StatusNotFound)
		return
	}

	cursor, err := db.Collection("complaints").Find(context.TODO(), bson.M{"userid": user.ID})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer cursor.Close(context.TODO())

	var userComplaints []Complaint
	for cursor.Next(context.TODO()) {
		var complaint Complaint
		if err := cursor.Decode(&complaint); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		userComplaints = append(userComplaints, complaint)
	}

	json.NewEncoder(w).Encode(userComplaints)
}

func getAllComplaintsForAdminHandler(w http.ResponseWriter, r *http.Request) {
	cursor, err := db.Collection("complaints").Find(context.TODO(), bson.M{})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer cursor.Close(context.TODO())

	var allComplaints []Complaint
	for cursor.Next(context.TODO()) {
		var complaint Complaint
		if err := cursor.Decode(&complaint); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		allComplaints = append(allComplaints, complaint)
	}

	json.NewEncoder(w).Encode(allComplaints)
}

func viewComplaintHandler(w http.ResponseWriter, r *http.Request) {
	complaintID := r.URL.Query().Get("complaintId")
	oid, err := primitive.ObjectIDFromHex(complaintID)
	if err != nil {
		http.Error(w, "Invalid complaint ID", http.StatusBadRequest)
		return
	}

	var complaint Complaint
	err = db.Collection("complaints").FindOne(context.TODO(), bson.M{"_id": oid}).Decode(&complaint)
	if err != nil {
		http.Error(w, "Complaint not found", http.StatusNotFound)
		return
	}

	json.NewEncoder(w).Encode(complaint)
}

func resolveComplaintHandler(w http.ResponseWriter, r *http.Request) {
	complaintID := r.URL.Query().Get("complaintId")
	oid, err := primitive.ObjectIDFromHex(complaintID)
	if err != nil {
		http.Error(w, "Invalid complaint ID", http.StatusBadRequest)
		return
	}

	var complaint Complaint
	err = db.Collection("complaints").FindOne(context.TODO(), bson.M{"_id": oid}).Decode(&complaint)
	if err != nil {
		http.Error(w, "Complaint not found", http.StatusNotFound)
		return
	}

	complaint.Resolved = true
	_, err = db.Collection("complaints").UpdateOne(context.TODO(), bson.M{"_id": oid}, bson.M{"$set": bson.M{"resolved": complaint.Resolved}})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	json.NewEncoder(w).Encode(complaint)
}

func main() {
	initDB()
	defer func() {
		if err := client.Disconnect(context.TODO()); err != nil {
			log.Fatal(err)
		}
	}()

	http.HandleFunc("/login", loginHandler)
	http.HandleFunc("/register", registerHandler)
	http.HandleFunc("/submitComplaint", submitComplaintHandler)
	http.HandleFunc("/getAllComplaintsForUser", getAllComplaintsForUserHandler)
	http.HandleFunc("/getAllComplaintsForAdmin", getAllComplaintsForAdminHandler)
	http.HandleFunc("/viewComplaint", viewComplaintHandler)
	http.HandleFunc("/resolveComplaint", resolveComplaintHandler)
	log.Fatal(http.ListenAndServe(":8080", nil))
}
