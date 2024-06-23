package cron

import (
	"fmt"
	"github.com/robfig/cron/v3"
	"time"
)

func NewCron() {
	c := cron.New(cron.WithSeconds())

	// Agregar una función que se ejecute cada 24 horas
	// El formato de cron es "segundo minuto hora día mes día-de-la-semana"
	_, err := c.AddFunc("0 0 0 * * *", func() {
		fmt.Println("Trabajo cron ejecutado a:", time.Now())
		// Aquí va tu lógica
	})

	if err != nil {
		fmt.Println("Error al agregar el trabajo cron:", err)
		return
	}

	// Iniciar el cron scheduler
	c.Start()

	// Mantener el programa en ejecución
	select {}
}
