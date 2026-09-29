# Pendientes de producto de MeppVP

## Anular puntos previamente sumados

Pedido: poder contrarrestar o anular una puntuación que ya se cargó, por ejemplo
cuando se pierde un bono, se invalida un objetivo o se corrige una carga.

Hoy se pueden restar puntos directos con un delta negativo y modificar el valor
actual de un campo. Falta una anulación explícita vinculada a la carga original,
con explicación y registro consultable. Esos mecanismos actuales no equivalen a
un historial de movimientos reversible.

Criterios propuestos para diseñar la mecánica:

- Distinguir corrección manual de pérdida de un bono por una regla del juego.
- Vincular cada anulación a un movimiento concreto; no borrar su historia.
- Conservar autor, motivo, fecha y efecto sobre el total del jugador.
- Evitar aplicar una misma anulación dos veces, incluso con reintentos o cargas
  simultáneas desde dos dispositivos.
- Mostrar el nuevo total a todos los participantes y definir quién puede anular
  cargas, incluyendo partidas pausadas o terminadas.
- Ejemplo de validación: un bono otorgado por +2 y luego anulado debe aportar 0;
  otras cargas del mismo jugador no deben cambiar.

## Modo duelo y condiciones de victoria

Pedido: una mecánica eficiente para juegos de dos jugadores, incluidos títulos
con victorias inmediatas, condiciones alternativas o estados compartidos.
No asumir que todo juego para dos tiene las mismas reglas, ni que el modo Dúo de
Wingspan coincide con otros juegos llamados Duel.

Criterios propuestos para diseñar la mecánica:

- Una configuración de partida para dos participantes, con acceso claro a cada
  lado y al estado compartido que requiera ese juego.
- Separar puntuación, avance en pistas y condiciones de victoria; una victoria
  inmediata no debe depender de tener el mayor total.
- Permitir configurar condiciones alternativas y desempates revisados para el
  reglamento elegido, sin inferirlos a partir del nombre del juego.
- Registrar el motivo de finalización y el ganador; definir qué sucede si se
  corrige o anula una acción que disparó una condición de victoria.
- Mantener coherencia ante acciones simultáneas y ofrecer pocas acciones claras
  para registrar cada cambio desde el móvil.
- Seleccionar un primer juego concreto y su reglamento para validar el flujo
  completo antes de generalizarlo a otros títulos.

Estado: ambos pedidos quedan anotados para diseño e implementación futura.
La incorporación de Wingspan base incluye solo su planilla y notas de reglas;
no implementa estas nuevas mecánicas.
