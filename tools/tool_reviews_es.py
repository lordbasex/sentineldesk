#!/usr/bin/env python3
"""Spanish for the method reviews in tool_reviews.py.

Keyed by tool name. A tool with an English review and no entry here falls back
to the English, which keeps the Spanish transcript usable while it is behind
rather than leaving a gap where the judgement should be.

The scores are not repeated: a number does not need translating, and duplicating
it would be one more place for the two files to disagree.
"""

REVIEWS_ES = {}


def review(tool, did, better):
    REVIEWS_ES[tool] = {"did": did, "better": better}


# --- ver el escritorio --------------------------------------------------------

review(
    "get_desktop_info",
    "Leyó display, encoder, resolución, gestor de ventanas, uptime y memoria en "
    "una sola llamada, desde /proc y la configuración en uso en vez de "
    "preguntarle a X.",
    "Mezcla datos que nunca cambian (display, gestor de ventanas) con otros que "
    "cambian cada segundo (memoria, carga). Quien haga polling de la memoria "
    "vuelve a leer las constantes cada vez. Habría que separar la mitad volátil, "
    "o decir cuándo se tomó cada campo.",
)
review(
    "get_screen_info",
    "Reportó la geometría y la cantidad de escritorios virtuales directo desde "
    "la conexión X.",
    "Ya es la fuente primaria: le pregunta a X, no parsea nada. Sólo mejoraría "
    "reportando la geometría por monitor, que este Xvfb de una sola cabeza no "
    "tiene forma de tener.",
)
review(
    "screenshot",
    "Capturó el framebuffer de forma nativa por desktop.GrabScreenshotPNG y "
    "devolvió PNG, con la misma captura disponible inline, a archivo, o "
    "empujada al navegador que esté mirando.",
    "Sin subproceso, sin pérdida por compresión, y un solo camino de captura "
    "para tres destinos. Para ir más lejos necesitaría damage tracking —"
    " devolver sólo lo que cambió desde la última llamada — que en un "
    "escritorio mayormente quieto recortaría un orden de magnitud los bytes que "
    "lee un modelo.",
)
review(
    "screenshot_region",
    "Recortó en el momento de capturar en vez de traer la pantalla y cortarla, "
    "así el costo es el rectángulo y no el display.",
    "La forma ya es la correcta. La mejora está en el nivel del que llama: "
    "combinarla con ui_find para poder nombrar una región ('el diálogo') en vez "
    "de medirla.",
)
review(
    "get_pixel_color",
    "Leyó un pixel por la conexión X — la forma más barata posible de verificar "
    "estado, sin que cruce ninguna imagen.",
    "Nada que mejorar en el mecanismo. Lo útil sería una compañera que espere a "
    "que un pixel cambie, lo que convertiría la lectura más barata en la "
    "primitiva de sincronización más barata.",
)
review(
    "read_screen_text",
    "Capturó a 2x y le pasó tesseract. Ese escalado es lo único que hace el OCR "
    "usable sobre tipografía de interfaz de 11px.",
    "La salida de esta misma corrida muestra el problema: 'SAVRANaAAAA SS', 'oO "
    "xXx', un botón leído como 'Go' sólo porque era grande. El OCR es el "
    "instrumento equivocado para un escritorio — está entrenado en documentos y "
    "un escritorio son íconos y degradados. Se gana el 2 porque es lo único que "
    "funciona sobre una aplicación sin ningún soporte de accesibilidad. Para ser "
    "excelente debería intentar primero el árbol AT-SPI y caer al OCR, diciendo "
    "cuál de los dos contestó para que quien llama sepa cuánto confiar.",
)
review(
    "find_text",
    "OCR con cajas por palabra, mapeando un texto de vuelta a coordenadas de "
    "pantalla que sirven para un clic.",
    "Hereda todas las debilidades de read_screen_text y suma una: un carácter "
    "mal leído da coordenadas de otra cosa, y quien llama no puede notarlo. "
    "Debería devolver la confianza por palabra de tesseract, y preferir una "
    "coincidencia AT-SPI cuando el texto exista en el árbol — ahí la respuesta "
    "es exacta en vez de probable.",
)
review(
    "get_mouse_position",
    "Consultó el puntero por la conexión X.",
    "Autoritativo y gratis. No hay nada que agregar.",
)
review(
    "get_active_window",
    "Le preguntó a EWMH qué ventana tiene el foco, y reportó degradado cuando "
    "ninguna lo tenía — que es un estado real del escritorio, no una falla.",
    "'no active window: exit status 1' filtra la shell que lo produjo. Que no "
    "haya foco es una respuesta, no un error: debería devolver null con una nota "
    "e isError en false, para que quien llama no tenga que decidir si el error "
    "significa roto o significa que nadie tiene el foco.",
)
review(
    "list_windows",
    "Listó cada ventana con id, escritorio, geometría, clase y título usando "
    "wmctrl.",
    "Hace shell-out a wmctrl y parsea columnas, o sea una dependencia de locale "
    "y de formato para datos que la conexión X ya tiene. Las mismas propiedades "
    "EWMH que lee están disponibles directo, e ir directo también sacaría el "
    "parseo por ancho fijo que se rompe con un título que tenga dos espacios.",
)
review(
    "list_desktops",
    "Listó los escritorios virtuales y marcó el actual, vía wmctrl.",
    "El mismo shell-out que list_windows y el mismo arreglo: "
    "_NET_DESKTOP_NAMES y _NET_CURRENT_DESKTOP están a una llamada X de "
    "distancia y no las puede arruinar un locale.",
)
review(
    "list_processes",
    "Corrió ps y filtró por substring.",
    "Un filtro por substring sobre una tabla de texto encuentra procesos cuyo "
    "argumento apenas menciona el nombre. Leer /proc directo daría coincidencia "
    "exacta sobre comm y argv, más los campos que quien llama suele querer "
    "después — rss, hora de inicio, padre — sin una segunda llamada.",
)
review(
    "is_running",
    "Preguntó si existe un proceso con ese nombre.",
    "Contesta lo que se le preguntó, pero un true/false pelado obliga a correr "
    "list_processes para saber algo más. Devolver la cantidad y los pids no "
    "costaría nada y sacaría esa segunda llamada.",
)
review(
    "list_installed_apps",
    "Leyó las entradas .desktop, así que la respuesta es lo que una persona "
    "vería en el menú y no lo que instaló dpkg.",
    "La fuente correcta: lista lo que se puede lanzar, no lo que existe en "
    "disco. Podría traer el ícono y las categorías de cada entrada, que es la "
    "diferencia entre una lista y algo sobre lo que un agente puede razonar.",
)
review(
    "get_audio_state",
    "Le preguntó a PulseAudio por el sink, el volumen y el estado de silencio.",
    "Reporta el sink del que graba el escritorio y no lo que hace el stream de "
    "cada aplicación, así que un agente no puede saber qué programa está "
    "haciendo ruido. Listar los sink inputs contestaría eso.",
)
review(
    "check_errors",
    "Recorrió el árbol de accesibilidad buscando alerts y diálogos, y después "
    "cualquier cosa cuyo texto suene a falla — estructura primero, heurística "
    "después.",
    "El instinto correcto: un programa gráfico no falla con un código de salida, "
    "pone un cuadro en la pantalla. Pero la mitad heurística tiene forma de "
    "inglés, así que un diálogo de error en español o portugués le es invisible "
    "— los mismos tres idiomas que la interfaz ya trae cerrarían eso.",
)
review(
    "wait",
    "Durmió, y desde la etapa 1 corta cuando se cancela la llamada en vez de "
    "dormir a través de la cancelación.",
    "Es la herramienta a la que recurre un modelo cuando no sabe qué está "
    "esperando, y una duración adivinada es o muy corta o desperdiciada. Cada "
    "uso que pueda nombrar una condición debería ser ui_wait_for, "
    "wait_for_window o wait_for_idle. Su descripción debería decirlo.",
)
review(
    "wait_for_idle",
    "Esperó a que la pantalla dejara de cambiar y la CPU se calmara, muestreando "
    "las dos cosas, y respeta la cancelación.",
    "La respuesta correcta al problema que `wait` adivina. Muestrea el "
    "framebuffer entero, así que un cursor parpadeando o un reloj lo mantienen "
    "despierto — limitar el chequeo de quietud a una región, o ignorar píxeles "
    "que cambian periódicamente, lo haría usable en un escritorio que nunca está "
    "del todo quieto.",
)

# --- el catálogo y la sala ----------------------------------------------------

review(
    "tool_search",
    "Ordenó el catálogo por keywords sobre nombre, categoría y descripción, con "
    "stopwords sacadas y unos veinte alias de categoría, y devolvió cada "
    "resultado con su schema y su riesgo para poder llamarlo sin un segundo "
    "round trip.",
    "Matching deliberadamente tonto, que es lo correcto para un corpus de 115 "
    "cadenas cortas — y aun así necesita que 'ssh' sea alcanzable desde la "
    "consulta, que es para lo que existen los alias. No puede contestar 'lo que "
    "escribe en un campo sin mover el mouse'. Rankear por los schemas que un "
    "modelo efectivamente llamó después, aprendido del action log, le ganaría a "
    "cualquier lista de alias escrita a mano.",
)
review(
    "action_log",
    "Devolvió el anillo de auditoría, y desde la etapa 1 cada entrada nombra la "
    "conexión y el cliente que hizo la llamada y lleva el tipo de denegación.",
    "En memoria y limitado a 2000 entradas, con JSONL sólo si se define "
    "ACTION_LOG. Un agente que quiera saber qué hizo hace una hora se encuentra "
    "con que ya no está. Hacer el archivo el default, rotado, no costaría nada y "
    "convertiría el log en algo en lo que apoyarse en vez de algo que hay que "
    "alcanzar a leer.",
)
review(
    "room_state",
    "Reportó quién está presente, quién tiene el control y si esta conexión "
    "puede inyectar — todo el estado de arbitraje en una lectura.",
    "La primitiva correcta para la invariante sobre la que está construido este "
    "proyecto. Lo que falta no está en esta herramienta sino al lado: no hay "
    "forma de que te avisen cuando cambia, así que un agente que pierde el "
    "control se entera porque lo rechazan. Eso es una notificación, no una "
    "lectura mejor.",
)

# --- tomar los controles ------------------------------------------------------

review(
    "request_control",
    "Le pidió el escritorio a la sala; se lo concedieron al instante porque "
    "nadie estaba manejando, y le habría puesto la pregunta a quienes miraban si "
    "hubiera habido alguien.",
    "Esto es el diseño funcionando: el control se reclama, nunca se asume, y "
    "preguntar es lo que hace visible cada traspaso. La mejora sería un campo de "
    "motivo que pueda llenar quien lo pide, para que el cartel que ve una "
    "persona diga para qué quiere el escritorio el agente y no solamente que lo "
    "quiere.",
)
review(
    "release_control",
    "Devolvió el escritorio, dejando los controles libres en vez de pasárselos a "
    "alguien.",
    "Correcto, incluida la parte que parece una omisión: no transferir significa "
    "que 'libre' es un estado en el que la sala puede quedarse, y nadie hereda "
    "un escritorio que no pidió. Una liberación diferida — devolver "
    "automáticamente después de N segundos sin actividad — evitaría que un "
    "agente que se cuelga a mitad de tarea se quede con los controles hasta que "
    "alguien lo note.",
)

# --- puntero y teclado --------------------------------------------------------

review(
    "mouse_move",
    "Movió el puntero por XTEST, el mismo camino que usa el DataChannel del "
    "navegador, así que una persona que esté mirando lo ve moverse.",
    "Un solo camino de inyección para los dos planos es justamente el punto: el "
    "puntero de un agente no es un segundo cursor invisible. Nada que mejorar a "
    "este nivel; suavizar un movimiento en pasos es cosa de quien llama.",
)
review(
    "mouse_click",
    "Hizo clic por XTEST, opcionalmente moviéndose primero, con botón y doble "
    "clic como argumentos.",
    "Correcto y completo para lo que un clic es. La parte débil es clickear a "
    "ciegas sobre coordenadas, y el arreglo no está acá: ui_click se dirige a un "
    "elemento y no puede errarle.",
)
review(
    "mouse_down",
    "Apretó y mantuvo un botón, dejando la pulsación abierta.",
    "Necesaria para todo lo que mouse_drag no puede expresar. También deja el "
    "escritorio en un estado apretado que sobrevive a la llamada, así que un "
    "agente que falla entre el down y el up deja el botón trabado — soltar los "
    "botones al liberar el control haría imposible ese estado irrecuperable.",
)
review(
    "mouse_up",
    "Soltó un botón que estaba apretado.",
    "El mismo problema de emparejamiento visto desde el otro lado: nada "
    "garantiza que llegue a correr. Ver mouse_down.",
)
review(
    "mouse_drag",
    "Apretó, movió y soltó como una sola llamada, que es lo que hace que un "
    "arrastre sea un arrastre y no tres eventos compitiendo.",
    "Correcto que sea una sola llamada. Se mueve en línea recta a una sola "
    "velocidad, y algunas interfaces distinguen un flick de un arrastre por la "
    "velocidad — una duración o cantidad de pasos opcional cubriría esos casos "
    "sin complicar el común.",
)
review(
    "mouse_scroll",
    "Scrolleó sintetizando pulsaciones de los botones 4 y 5, que es como X "
    "expresa la rueda.",
    "Botón 4/5 es la codificación vieja; los toolkits modernos esperan smooth "
    "scrolling de XInput2, y una aplicación que sólo escuche eso no se mueve. "
    "Mandar eventos de smooth scroll con fallback a botones arreglaría las "
    "aplicaciones que hoy no puede scrollear.",
)
review(
    "type_text",
    "Escribió el texto por el inyector, remapeando keycodes al vuelo para los "
    "caracteres que el layout activo no tiene en ninguna tecla.",
    "Ese remapeo es lo que la hace funcionar con acentos y símbolos en vez de "
    "sólo ASCII, y es el motivo para preferirla antes que componer llamadas a "
    "key_combo. Para un texto largo sigue siendo un evento de tecla sintético "
    "por carácter; poner el portapapeles y pegar sería más rápido, pero cambia "
    "lo que ve el escritorio, así que este es el default honesto.",
)
review(
    "key_combo",
    "Apretó una combinación por nombre de keysym de X, resolviendo cada nombre "
    "contra el keymap vivo.",
    "Nombrar teclas por keysym es lo correcto — es la capa que sobrevive a un "
    "cambio de layout. Un keysym que el mapa actual no tenga se descarta "
    "silenciosamente, que es cómo una combinación puede no hacer nada; rechazar "
    "la llamada nombrando el que falta convertiría una falla muda en un mensaje.",
)

# --- lanzar, ejecutar, grabar -------------------------------------------------

review(
    "launch_app",
    "Arrancó un programa desacoplado con setsid, así que cerrar la conexión MCP "
    "no se lleva puesta la aplicación, y con as_root pasando por sudo -E para "
    "conservar DISPLAY.",
    "Desacoplar es lo correcto y el motivo está escrito. Lo que no puede decir "
    "es si el programa realmente arrancó: vuelve apenas la shell hizo fork, y "
    "un comando que muere al instante se ve idéntico a uno que anduvo. "
    "open_app_and_wait existe por ese hueco, lo que ya indica que esta debería "
    "al menos devolver el pid y si seguía vivo un momento después.",
)
review(
    "activate_window",
    "Enfocó y trajo al frente una ventana por id usando wmctrl.",
    "Otro shell-out para un solo mensaje EWMH. Mandar _NET_ACTIVE_WINDOW "
    "directo sería una llamada X sin proceso y sin salida que parsear, y además "
    "permitiría reportar si el gestor de ventanas atendió el pedido — cosa que "
    "el código de salida de wmctrl no distingue de haber preguntado por una "
    "ventana que ya no existe.",
)
review(
    "run_command",
    "Corrió un comando por sh -c con deadline, capturando stdout, stderr y el "
    "código de salida, matando el proceso cuando se cancela la llamada y "
    "reportando su salida como progreso mientras corre.",
    "Todo lo que una herramienta de shell debería ser: un deadline real, un kill "
    "real, y la salida en streaming en vez de retenida hasta el final. Es la "
    "herramienta más peligrosa del catálogo y está clasificada como tal. Lo "
    "único que falta es una forma argv al lado de la de string, para poder pasar "
    "un nombre de archivo con espacios sin pensar en comillas.",
)
review(
    "start_recording",
    "Arrancó una grabación lanzando gst-launch-1.0 en su propio grupo de "
    "procesos, capturando y codificando la pantalla una segunda vez en paralelo "
    "al stream en vivo.",
    "Este es el único lugar donde el proyecto hace lo que su propia arquitectura "
    "dice que no hace: GStreamer corre in-process en todos lados menos acá, "
    "donde es un hijo gst-launch. Además implica leer el framebuffer y "
    "codificar H.264 dos veces para una sola pantalla, que en un escritorio "
    "cargado es lo más caro que hace el daemon. start_restream ya muestra la "
    "respuesta: hace un tee del pipeline vivo y no codifica nada extra. La "
    "grabación debería hacer lo mismo, y de paso arrancaría al instante en vez "
    "de esperar a que levante un segundo pipeline.",
)
review(
    "stop_recording",
    "Señalizó el grupo de procesos del pipeline para que el contenedor se cierre "
    "correctamente, y reportó ruta y tamaño.",
    "Parar limpio en vez de matar es lo que hace que el mp4 sea reproducible, y "
    "ese detalle es fácil de errar. Hereda el problema del segundo pipeline de "
    "start_recording: el stop tiene que esperar a que drene un proceso que no "
    "existiría si la grabación saliera por tee del encode vivo.",
)
review(
    "get_recording_status",
    "Reportó si hay una grabación corriendo con segundos transcurridos, tamaño "
    "actual y ruta.",
    "El tamaño en disco es un buen proxy de 'realmente está escribiendo', que un "
    "booleano no daría. No puede decir si se están perdiendo cuadros, y una "
    "grabación que corre pero se está quedando sin datos es indistinguible de "
    "una sana hasta que la reproducís.",
)
review(
    "list_recordings",
    "Listó los archivos terminados con tamaño y fecha de modificación.",
    "La forma correcta. Reporta lo que hay en disco y no lo que es reproducible "
    "— un archivo que dejó una grabación que nunca paró limpio se ve igual que "
    "uno bueno. Sondear la duración del contenedor los separaría.",
)

# --- portapapeles -------------------------------------------------------------

review(
    "get_clipboard",
    "Leyó la selección CLIPBOARD de X con xclip, tratando un portapapeles vacío "
    "y una selección sin dueño como cosas normales y no como errores.",
    "La distinción que hace es correcta: que nadie sea dueño de la selección no "
    "es una falla. Pero es un subproceso por lectura para algo que la conexión X "
    "puede hacer, y sólo texto — una imagen o una ruta de archivo en el "
    "portapapeles le son invisibles, que es justo lo que más probablemente tenga "
    "una persona que copió algo para el agente.",
)
review(
    "set_clipboard",
    "Escribió texto en la selección CLIPBOARD de X con xclip, que queda vivo en "
    "segundo plano para ser dueño de la selección.",
    "Descarta el resultado — `_ = cmd.Run()` — así que una escritura fallida se "
    "reporta como éxito y el agente sigue adelante a pegar algo que no está. "
    "Eso solo es la diferencia entre un dos y un cuatro. Ser dueño de la "
    "selección desde adentro del daemon arreglaría la honestidad y el proceso "
    "xclip colgado al mismo tiempo.",
)

# --- ventanas -----------------------------------------------------------------

review(
    "wait_for_window",
    "Hizo polling buscando una ventana cuyo título coincida, con deadline, y "
    "corta cuando se cancela la llamada.",
    "La herramienta correcta a la que recurrir en vez de adivinar con `wait`, y "
    "el polling es la forma equivocada de implementarla. X puede avisar por "
    "evento cuando se crea una ventana o cambia su título; suscribirse a eso en "
    "la ventana raíz la haría exacta en vez de 'dentro de 300ms', y no costaría "
    "nada mientras espera.",
)
review(
    "move_window",
    "Movió una ventana con wmctrl -e.",
    "Shell-out y un string de geometría. Tampoco puede expresar 'mover sin "
    "redimensionar' salvo pasando centinelas -1 internamente, que es señal de "
    "que la llamada de abajo tiene la forma equivocada. Un ConfigureWindow "
    "directo toma los campos que se están fijando y nada más.",
)
review(
    "resize_window",
    "Redimensionó por el mismo camino de wmctrl -e.",
    "Igual que move_window, y comparte su helper. Conviene arreglarlas juntas y "
    "no por separado: una sola llamada directa de geometría reemplaza a las dos.",
)
review(
    "minimize_window",
    "Minimizó con xdotool windowminimize.",
    "Una segunda herramienta de shell para lo que las demás hacen con wmctrl, "
    "así que la familia ahora depende de dos programas externos para hacer un "
    "solo tipo de cosa. _NET_WM_STATE_HIDDEN por el mismo camino que el resto "
    "sacaría la dependencia de xdotool por completo.",
)
review(
    "maximize_window",
    "Agregó los estados EWMH maximized_vert y maximized_horz con wmctrl.",
    "Mecanismo correcto — le pide al gestor de ventanas en vez de redimensionar "
    "al tamaño de la pantalla, así que una ventana maximizada sigue maximizada "
    "cuando cambia la resolución. Sólo el shell-out la separa de un cinco.",
)
review(
    "restore_window",
    "Sacó los dos estados de maximizado.",
    "La inversa correcta de maximizar, y no intenta recordar una geometría "
    "previa que el gestor de ventanas ya conoce. Misma salvedad del shell-out.",
)
review(
    "fullscreen_window",
    "Alternó _NET_WM_STATE_FULLSCREEN.",
    "Alternar es el problema: un agente que no puede ver el estado actual no "
    "sabe para qué lado fue, así que 'poné esto en pantalla completa' requiere "
    "una lectura y una adivinanza. Debería tomar el estado que quiere — add, "
    "remove o toggle — como ya hace window_set_state.",
)
review(
    "set_window_desktop",
    "Movió una ventana a un escritorio virtual con wmctrl -t.",
    "Mecanismo correcto, y acepta el índice de escritorio que reportan las otras "
    "herramientas, así que los números coinciden en toda la familia. Otra vez el "
    "shell-out.",
)
review(
    "switch_desktop",
    "Cambió el escritorio virtual actual con wmctrl -s.",
    "Igual. Lo que valdría agregar es cambiar por nombre y no por índice, dado "
    "que _NET_DESKTOP_NAMES ya es lo que lee list_desktops.",
)
review(
    "window_properties",
    "Leyó las propiedades X crudas de una ventana.",
    "Esta es la única de la familia que va a la fuente, y es la más útil de "
    "todas para un agente que intenta entender una ventana que no abrió él. "
    "Nada que cambiar.",
)
review(
    "window_hierarchy",
    "Recorrió el árbol de ventanas de X, reportando padres, hijos y "
    "override-redirect.",
    "La respuesta correcta para preguntas que la lista EWMH no puede expresar — "
    "tooltips, menús y popups son override-redirect y nunca aparecen en "
    "list_windows. Combinarla con el árbol de accesibilidad sería el paso "
    "siguiente, pero eso es una herramienta nueva y no un cambio a esta.",
)
review(
    "window_set_state",
    "Fijó un estado EWMH — above, below, sticky, shaded, skip_taskbar y el resto "
    "— con un add, remove o toggle explícito.",
    "La herramienta mejor formada de la familia de ventanas: nombra el estado y "
    "la acción en vez de esconder los dos detrás de un verbo, que es lo que "
    "fullscreen_window debería haber hecho. Podría reemplazar de plano a "
    "maximize, restore y fullscreen, dejando una herramienta donde hay cuatro.",
)
